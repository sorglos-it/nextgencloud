package store

import (
	"bytes"
	"io"
	"strconv"
	"strings"
)

// A UI tar is how the API keeps an app's UI (dbapi.BlobUI) and sends it to
// the shell (ngc.desktop/ui). UIWriter writes it and WalkUI reads it. Both
// know exactly one form, the one below, so the shell's reader can follow the
// same rules line by line (vectors: ui.json):
//
//   - The archive is a sequence of 512-byte blocks: its entries, then the end
//     marker of two blocks of NUL bytes, then nothing.
//   - An entry is a header block and, for a file, its data, padded with NUL
//     bytes to a whole block. A name longer than 100 bytes (the trailing "/"
//     of a directory counted) goes into a PAX header, a block of typeflag 'x'
//     directly before the entry's header, and only such a name does.
//   - Every header block has this layout; NUL is the byte 0, and a field
//     given as a string has exactly that value:
//     0–99 name: 1 to 100 bytes, then NUL bytes to the end of the field;
//     all NUL in a PAX header and in the header after it;
//     100–107 mode: "0000644" NUL, for a directory "0000755" NUL;
//     108–115 uid and 116–123 gid: "0000000" NUL;
//     124–135 size: 11 octal digits and NUL; 0 for a directory; for a PAX
//     header the length of its data, at most 512;
//     136–147 mtime: "00000000000" NUL;
//     148–155 checksum: 6 octal digits, NUL and a space; the sum of the 512
//     bytes with these 8 counted as spaces;
//     156 typeflag: '0' file, '5' directory, 'x' PAX header;
//     257–262 magic: "ustar" NUL; 263–264 version: "00";
//     every other byte NUL (linkname 157–256, uname, gname, devmajor,
//     devminor, prefix 345–499, and 500–511).
//   - The data of a PAX header is exactly one record, "<length> path=<name>\n",
//     where length is the byte count of the whole record in decimal without
//     leading zeros, then NUL bytes to the end of the block.
//   - Names: a file "ui/<ValidPath>", a directory "ui/" or "ui/<ValidPath>/".
//     The path fn gets has no trailing "/".
//   - At most MaxEntries entries (a PAX header and its entry count as one), a
//     file at most MaxUIFile bytes and all files at most MaxUITotal, each
//     checked from the header before anything after it is read.
//   - The entries form a tree, in any order: no path twice, no entry under a
//     file, no file where a directory is needed, and at most MaxDirs
//     directories, each counted once whether an entry names it or a path
//     implies it ("ui/a/b" implies "ui" and "ui/a").
//
// So a UI tar has at most about 84 MiB, and every entry is read in one pass
// with a fixed amount of memory besides the names kept for the tree rules.
const (
	blockSize   = 512
	uiNameField = 100 // longest name a header holds
)

// uiHeader returns a header block of a UI tar: typeflag flag, name (empty
// in a PAX header and in the header after it) and size.
func uiHeader(flag byte, name string, size int64) *[blockSize]byte {
	b := new([blockSize]byte)
	copy(b[0:100], name)
	mode := "0000644\x00"
	if flag == '5' {
		mode = "0000755\x00"
	}
	copy(b[100:108], mode)
	copy(b[108:124], "0000000\x000000000\x00")
	copy(b[124:136], octalField(size, 12))
	copy(b[136:148], "00000000000\x00")
	b[156] = flag
	copy(b[257:265], "ustar\x0000")
	copy(b[148:156], octalField(int64(headerSum(b)), 7)+" ")
	return b
}

// octalField formats v as octal digits and a NUL, width bytes in all.
func octalField(v int64, width int) string {
	s := strconv.FormatInt(v, 8)
	return strings.Repeat("0", width-1-len(s)) + s + "\x00"
}

// headerSum is the sum of a header's bytes with its checksum field counted
// as eight spaces.
func headerSum(b *[blockSize]byte) int {
	sum := 8 * int(' ')
	for i, c := range b {
		if i < 148 || i >= 156 {
			sum += int(c)
		}
	}
	return sum
}

// uiEntry is a checked header of a UI tar.
type uiEntry struct {
	flag byte
	name string
	size int64
}

// parseUIHeader checks b against the layout uiHeader writes: every field has
// one value, but the name, the size and the checksum.
func parseUIHeader(b *[blockSize]byte) (uiEntry, bool) {
	e := uiEntry{flag: b[156]}
	mode := "0000644\x00"
	switch e.flag {
	case '0', 'x':
	case '5':
		mode = "0000755\x00"
	default:
		return uiEntry{}, false
	}
	n := bytes.IndexByte(b[0:100], 0)
	if n < 0 {
		n = 100
	}
	size, okSize := octalValue(b[124:136])
	sum, okSum := octalValue(b[148:155])
	if !okSize || !okSum || b[155] != ' ' || int(sum) != headerSum(b) || !allNUL(b[n:100]) ||
		string(b[100:108]) != mode || string(b[108:124]) != "0000000\x000000000\x00" ||
		string(b[136:148]) != "00000000000\x00" || !allNUL(b[157:257]) ||
		string(b[257:265]) != "ustar\x0000" || !allNUL(b[265:]) || (e.flag == '5' && size != 0) {
		return uiEntry{}, false
	}
	e.name, e.size = string(b[:n]), size
	return e, true
}

// octalValue reads a field of octal digits that ends in one NUL.
func octalValue(f []byte) (int64, bool) {
	if len(f) < 2 || f[len(f)-1] != 0 {
		return 0, false
	}
	var v int64
	for _, c := range f[:len(f)-1] {
		if c < '0' || c > '7' {
			return 0, false
		}
		v = v<<3 | int64(c-'0')
	}
	return v, true
}

func allNUL(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

// paxPathRecord is the one PAX record of a UI tar: "<length> path=<name>\n",
// length counting the whole record.
func paxPathRecord(name string) string {
	s := " path=" + name + "\n"
	n := len(s) + 1
	for len(strconv.Itoa(n))+len(s) != n {
		n = len(strconv.Itoa(n)) + len(s)
	}
	return strconv.Itoa(n) + s
}

// parsePAXPath reads the data of a PAX header: exactly paxPathRecord(name),
// so the length has no sign and no leading zero and counts the record.
func parsePAXPath(data []byte) (string, bool) {
	sp := bytes.IndexByte(data, ' ')
	if sp < 0 {
		return "", false
	}
	rest, ok := bytes.CutPrefix(data[sp:], []byte(" path="))
	if !ok || len(rest) == 0 {
		return "", false
	}
	name := string(rest[:len(rest)-1])
	return name, paxPathRecord(name) == string(data)
}

// uiName checks the name of an entry and returns its path: a file
// "ui/<ValidPath>", a directory "ui/" or "ui/<ValidPath>/" without the "/".
func uiName(name string, dir bool) (string, bool) {
	if dir {
		p, ok := strings.CutSuffix(name, "/")
		return p, ok && (p == "ui" || uiPath(p))
	}
	return name, uiPath(name)
}

// WalkUI reads a UI tar (see the rules above) and calls fn for every regular
// file with its data. Every failure is ErrPackage or fn's own error; after
// one the caller discards everything fn received.
func WalkUI(r io.Reader, fn func(f PackFile, body io.Reader) error) error {
	return walkUI(r, fn, nil)
}

// walkUI is WalkUI that also calls dir, if set, for every directory.
func walkUI(r io.Reader, fn func(f PackFile, body io.Reader) error, dir func(path string)) error {
	var blk [blockSize]byte
	read := func() bool {
		_, err := io.ReadFull(r, blk[:])
		return err == nil
	}
	entries := newTree()
	var total int64
	for n := 1; ; n++ {
		if !read() {
			return ErrPackage
		}
		if allNUL(blk[:]) {
			// The end marker: one more zero block, then nothing.
			var one [1]byte
			if !read() || !allNUL(blk[:]) {
				return ErrPackage
			}
			if k, err := io.ReadFull(r, one[:]); k != 0 || err != io.EOF {
				return ErrPackage
			}
			return nil
		}
		e, ok := parseUIHeader(&blk)
		if !ok {
			return ErrPackage
		}
		name := e.name
		if e.flag == 'x' {
			// A name too long for a header, then the entry's own header.
			if name != "" || e.size > blockSize || !read() || !allNUL(blk[e.size:]) {
				return ErrPackage
			}
			if name, ok = parsePAXPath(blk[:e.size]); !ok || len(name) <= uiNameField || !read() {
				return ErrPackage
			}
			if e, ok = parseUIHeader(&blk); !ok || e.flag == 'x' || e.name != "" {
				return ErrPackage
			}
		} else if name == "" {
			return ErrPackage
		}
		isDir := e.flag == '5'
		path, ok := uiName(name, isDir)
		if !ok || n > MaxEntries {
			return ErrPackage
		}
		if !isDir {
			total += e.size
			if e.size > MaxUIFile || total > MaxUITotal {
				return ErrPackage
			}
		}
		if !entries.add(path, !isDir) {
			return ErrPackage
		}
		if isDir {
			if dir != nil {
				dir(path)
			}
			continue
		}
		body := &uiBody{r: r, left: e.size}
		if err := fn(PackFile{Path: path, Size: e.size, Kind: KindUI}, body); err != nil {
			return err
		}
		if _, err := io.Copy(io.Discard, body); err != nil {
			return ErrPackage
		}
		if pad := (blockSize - e.size%blockSize) % blockSize; pad > 0 {
			if _, err := io.ReadFull(r, blk[:pad]); err != nil || !allNUL(blk[:pad]) {
				return ErrPackage
			}
		}
	}
}

// uiBody is the data of one file for fn: exactly left bytes of r; r ending
// before them is ErrPackage.
type uiBody struct {
	r    io.Reader
	left int64
}

func (b *uiBody) Read(p []byte) (int, error) {
	if b.left <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, err := b.r.Read(p)
	b.left -= int64(n)
	if err != nil && (err != io.EOF || b.left > 0) {
		return n, ErrPackage
	}
	return n, nil
}

// UIWriter writes a UI tar in the one form WalkUI reads. It keeps the rules
// of WalkUI as it goes and refuses an entry that breaks one with
// ErrPackage; after any error it refuses everything.
type UIWriter struct {
	w       io.Writer
	entries *tree
	n       int
	total   int64
	err     error
}

// NewUIWriter returns a UIWriter that writes to w.
func NewUIWriter(w io.Writer) *UIWriter { return &UIWriter{w: w, entries: newTree()} }

// Dir adds the directory "ui" or "ui/<ValidPath>".
func (u *UIWriter) Dir(path string) error { return u.add(path, true, 0, nil) }

// File adds the file "ui/<ValidPath>" of size bytes, read from r.
func (u *UIWriter) File(path string, size int64, r io.Reader) error {
	return u.add(path, false, size, r)
}

// Close writes the end marker. It does not close the writer under it.
func (u *UIWriter) Close() error {
	if u.err != nil {
		return u.err
	}
	u.err = ErrPackage // nothing may follow the end marker
	_, err := u.w.Write(make([]byte, 2*blockSize))
	return err
}

func (u *UIWriter) add(path string, dir bool, size int64, r io.Reader) error {
	if u.err != nil {
		return u.err
	}
	u.err = ErrPackage // until the entry is written in full
	name, flag := path, byte('0')
	if dir {
		name, flag = path+"/", '5'
	}
	if _, ok := uiName(name, dir); !ok || size < 0 || size > MaxUIFile || u.total+size > MaxUITotal {
		return u.err
	}
	if u.n++; u.n > MaxEntries || !u.entries.add(path, !dir) {
		return u.err
	}
	u.total += size
	var head []byte
	if len(name) > uiNameField {
		record := paxPathRecord(name)
		head = append(uiHeader('x', "", int64(len(record)))[:], pad512([]byte(record))...)
		name = ""
	}
	if _, err := u.w.Write(append(head, uiHeader(flag, name, size)[:]...)); err != nil {
		u.err = err
		return err
	}
	if !dir {
		if _, err := io.CopyN(u.w, r, size); err != nil {
			if err == io.EOF {
				err = ErrPackage // r had fewer bytes than size
			}
			u.err = err
			return err
		}
		if pad := (blockSize - size%blockSize) % blockSize; pad > 0 {
			if _, err := u.w.Write(make([]byte, pad)); err != nil {
				u.err = err
				return err
			}
		}
	}
	u.err = nil
	return nil
}

// pad512 appends NUL bytes to b up to a whole block.
func pad512(b []byte) []byte {
	return append(b, make([]byte, (blockSize-len(b)%blockSize)%blockSize)...)
}
