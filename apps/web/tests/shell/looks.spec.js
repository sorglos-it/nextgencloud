import { test, expect, signIn, skipWithoutLogin } from "../fixtures/page.js";

// Die Vorlagen (docs/gestaltung.md, Punkt 8): ein Ordner unter dem Webserver,
// je Vorlage ein Unterordner, der das ganze Aussehen mitbringt, und die
// Auswahl im Startmenü unter „Aussehen".
//
// WAS HIER ECHT IST: der Ordner public/assets/looks/ mit zwei Vorlagen,
// ausgeliefert vom selben Server wie die Seite; die Auswahl; das Stylesheet,
// das wirklich hinter shell.css hängt und die Marken umschreibt; und das
// Aussehen, das eine Vorlage für das Terminal mitbringt und das die
// Oberfläche in den abgeschotteten Rahmen hängt, ohne dessen Regel anzufassen.
//
// WAS HIER NICHT GEMESSEN WIRD: das Hochladen. Der schreibende halbe Weg
// gehört dem Server und steht in docs/vorlagen-und-der-rahmen.md als offener
// Punkt - kein Fall unten tut so, als wäre er da.
test.beforeEach(async ({ shell }) => {
  await skipWithoutLogin(shell);
  await signIn(shell);
  await expect(shell.locator("#taskbar")).toBeVisible();
});

const radius = (shell) => shell.evaluate(() =>
  getComputedStyle(document.documentElement).getPropertyValue("--radius").trim());
const accent = (shell) => shell.evaluate(() =>
  getComputedStyle(document.documentElement).getPropertyValue("--accent").trim());

test("beide Vorlagen stehen im Startmenü, neben dem mitgelieferten Aussehen", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  // Mitgeliefert, Klar, Kante: der erste Eintrag ist kein Ordner, sondern
  // das Aussehen, das NGC selbst mitbringt.
  await expect(shell.locator("#smlook .looks button")).toHaveCount(3);
  await expect(shell.locator('#smlook .looks button[data-look=""]')).toHaveText("Mitgeliefert");
  await expect(shell.locator('#smlook .looks button[data-look="klar"]')).toHaveText("Klar");
  await expect(shell.locator('#smlook .looks button[data-look="kante"]')).toHaveText("Kante");
  // Ohne Wahl ist das Mitgelieferte in Kraft, und nur das ist markiert.
  await expect(shell.locator("#smlook .looks button.on")).toHaveCount(1);
  await expect(shell.locator('#smlook .looks button[data-look=""]')).toHaveClass(/on/);
});

test("ein Benutzer schaltet zwischen den beiden um und sieht den Unterschied", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  // Das mitgelieferte Aussehen: die Marke aus css/shell.css.
  expect(await radius(shell)).toBe("10px");

  await shell.getByRole("button", { name: "Vorlage Klar", exact: true }).click();
  // Das Stylesheet hängt am Dokument, vom eigenen Server, hinter shell.css.
  const href = await shell.locator("#looksheet").getAttribute("href");
  expect(href).toBe("/assets/looks/klar/shell.css");
  await expect.poll(() => radius(shell)).toBe("18px");

  await shell.getByRole("button", { name: "Vorlage Kante", exact: true }).click();
  await expect.poll(() => radius(shell)).toBe("0px");
  await expect(shell.locator('#smlook .looks button[data-look="kante"]')).toHaveClass(/on/);
  await expect(shell.locator("#smlook .looks button.on")).toHaveCount(1);

  // Und zurück zum Mitgelieferten: das Stylesheet ist wieder weg, nicht nur
  // überschrieben.
  await shell.getByRole("button", { name: "Vorlage Mitgeliefert", exact: true }).click();
  await expect(shell.locator("#looksheet")).toHaveCount(0);
  await expect.poll(() => radius(shell)).toBe("10px");
});

test("die gewählte Vorlage überlebt das Neuladen", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("button", { name: "Vorlage Kante", exact: true }).click();
  await shell.reload();
  await signIn(shell);
  await expect(shell.locator("#taskbar")).toBeVisible();
  // Sie steht vor dem ersten Malen da (main.js, initLook), nicht erst danach.
  expect(await radius(shell)).toBe("0px");
  await shell.getByRole("button", { name: "Start" }).click();
  await expect(shell.locator('#smlook .looks button[data-look="kante"]')).toHaveClass(/on/);
});

test("eine Vorlage hebt Hell/Dunkel und die Farbe des Benutzers nicht auf", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("button", { name: "Vorlage Klar", exact: true }).click();
  // Die Farbe ist seine Wahl (Punkt 7 und die Farbwahl daneben): sie steht
  // als Angabe am Element und schlägt jede Regel der Vorlage.
  await shell.getByRole("button", { name: "Farbe Pink", exact: true }).click();
  expect(await accent(shell)).toBe("#e14f8a");

  // Hell und dunkel hängen an data-theme. Die Vorlage bedient beide Fälle und
  // zieht die Wahl nicht an sich: die Umschaltung wirkt weiter, und die
  // Vorlage bleibt dabei in Kraft.
  const bg = () => shell.evaluate(() =>
    getComputedStyle(document.documentElement).getPropertyValue("--win-bg").trim());
  const dark = await bg();
  await shell.getByRole("button", { name: "Hell", exact: true }).click();
  const light = await bg();
  expect(light).not.toBe(dark);
  // Beides sind Werte DER VORLAGE, nicht die von css/shell.css.
  expect(dark).toBe("#1b2030");
  expect(light).toBe("#fdfdff");
  expect(await radius(shell)).toBe("18px");
  expect(await accent(shell)).toBe("#e14f8a");
});

// ------------------------------------------------- das Aussehen einer App

// Das Terminal läuft im abgeschotteten Rahmen gegen ein unterschriebenes
// Archiv. Liegt in der Vorlage ein Ordner für die App, wird SEIN Aussehen
// geladen statt des mitgelieferten - und zwar so, dass der Riegel des Rahmens
// zubleibt (docs/vorlagen-und-der-rahmen.md).
async function terminal(shell) {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Terminal", exact: true }).click();
  const frame = shell.frameLocator(".appframe");
  const out = frame.locator(".out");
  await expect(out).toBeVisible();
  return out;
}

const cornerOf = (out) => out.evaluate((e) => getComputedStyle(e).borderTopLeftRadius);

test("die Vorlage ersetzt das Aussehen der App im Rahmen", async ({ shell }) => {
  // Zuerst ohne Vorlage: das mitgelieferte app.css des Terminals.
  expect(await cornerOf(await terminal(shell))).toBe("8px");

  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("button", { name: "Vorlage Klar", exact: true }).click();
  // Ein Fenster schreibt sein Dokument genau einmal; die Vorlage greift beim
  // nächsten Öffnen. Also neu öffnen und dann messen.
  await shell.reload();
  await signIn(shell);
  await expect(shell.locator("#taskbar")).toBeVisible();
  expect(await cornerOf(await terminal(shell))).toBe("16px");

  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("button", { name: "Vorlage Kante", exact: true }).click();
  await shell.reload();
  await signIn(shell);
  await expect(shell.locator("#taskbar")).toBeVisible();
  expect(await cornerOf(await terminal(shell))).toBe("0px");
});

test("der Rahmen behält seinen eigenen Ursprung und macht keine Verbindung", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("button", { name: "Vorlage Klar", exact: true }).click();
  await shell.reload();
  await signIn(shell);
  await expect(shell.locator("#taskbar")).toBeVisible();
  const out = await terminal(shell);
  // Der Ursprung des Rahmens ist weiter undurchsichtig: die Vorlage hat ihn
  // nicht geöffnet.
  expect(await out.evaluate(() => String(window.origin))).toBe("null");
  // Und das Stylesheet der Vorlage steht als data:-Adresse im Kopf, als
  // LETZTES - geholt hat es die Oberfläche, nicht der Rahmen.
  const sheets = await out.evaluate(() =>
    [...document.head.querySelectorAll('link[rel="stylesheet"]')].map((l) => l.getAttribute("href")));
  expect(sheets.length).toBeGreaterThan(1);
  for (const h of sheets) expect(h.startsWith("data:")).toBe(true);
  expect(sheets[sheets.length - 1]).toContain("data:text/css;base64,");
});

test("ein url() der Vorlage, das nicht data: ist, kommt nicht in den Rahmen", async ({ shell }) => {
  // Geputzt wird in look.js, bevor irgendetwas gebaut wird. Gemessen am
  // Modul selbst, weil keine der beiden ausgelieferten Vorlagen eine Adresse
  // nach draußen trägt - und eine ins Repo zu legen, um sie zu messen, hieße
  // eine Vorlage auszuliefern, die etwas versucht.
  const got = await shell.evaluate(async () => {
    const m = await import("/assets/js/shell/look.js");
    return {
      out: m.clean('a{background:url(https://example.invalid/x.png)}'),
      data: m.clean('a{background:url("data:image/gif;base64,R0lGOD")}'),
      imp: m.clean('@import url(https://example.invalid/x.css);a{color:red}'),
      id: [m.validLookID("klar"), m.validLookID("../x"), m.validLookID("a/b"), m.validLookID("")],
    };
  });
  expect(got.out).toBe('a{background:url("data:,")}');
  expect(got.data).toBe('a{background:url("data:image/gif;base64,R0lGOD")}');
  expect(got.imp).toBe("a{color:red}");
  expect(got.id).toEqual([true, false, false, false]);
});
