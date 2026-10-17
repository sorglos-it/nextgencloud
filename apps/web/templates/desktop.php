<?php
declare(strict_types=1);
// The desktop page. Its lang attribute and its one sentence come out of
// lang/, like every other text: a page that claims "de" while it shows
// French is read out wrongly by a screen reader.
use Ngc\Lang;
?><!DOCTYPE html>
<html lang="<?= htmlspecialchars(Lang::tag(), ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8') ?>" data-theme="dark">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="dark light">
<title>NGC</title>
<link rel="stylesheet" href="/assets/css/shell.css">
</head>
<body>
<div id="root"></div>
<noscript><?= htmlspecialchars(Lang::t('page.noscript'), ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8') ?></noscript>
<script type="module" src="/assets/js/shell/main.js"></script>
</body>
</html>
