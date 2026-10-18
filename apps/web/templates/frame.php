<?php
declare(strict_types=1);
// An app window before the app is in it. Everything it holds is the
// bootstrap; the shell hands it the app's proven user interface by message,
// and the bootstrap writes it. See classes/Page.php (FRAME_CSP) for the
// policy this document is served with, and assets/js/shell/frame-boot.js for
// why it may write at all.
/** @var string $boot */
?><!DOCTYPE html>
<html lang="de">
<head>
<meta charset="utf-8">
<title>NGC</title>
<script src="<?= htmlspecialchars($boot, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8') ?>"></script>
</head>
<body></body>
</html>
