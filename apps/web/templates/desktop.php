<?php
declare(strict_types=1);
// The desktop page. Its lang attribute, its one sentence and the TABLES THE
// SHELL READS all come out of lang/: a page that claims "de" while it shows
// French is read out wrongly by a screen reader, and public/assets/js/lang.js
// takes its two tables from the block below rather than fetching them, so
// t() is a plain synchronous call everywhere (the reason stands in that file).
// The block is JSON in a script element the browser does not execute, so the
// policy of this page was not loosened for it. It stands in the HEAD and not
// in the body: a case of shell/desktop-hostile.spec.js reads
// document.body.textContent to prove a refused app name is nowhere on the
// page, and a table of three hundred sentences in the body made that text
// hold words the page never showed. Measured - the case went red, and it was
// right to.
use Ngc\Lang;
?><!DOCTYPE html>
<html lang="<?= htmlspecialchars(Lang::tag(), ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8') ?>" data-theme="dark">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="dark light">
<title>NGC</title>
<link rel="stylesheet" href="/assets/css/shell.css">
<script type="application/json" id="ngc-lang"><?= Lang::page() ?></script>
</head>
<body>
<div id="root"></div>
<noscript><?= htmlspecialchars(Lang::t('page.noscript'), ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8') ?></noscript>
<script type="module" src="/assets/js/shell/main.js"></script>
</body>
</html>
