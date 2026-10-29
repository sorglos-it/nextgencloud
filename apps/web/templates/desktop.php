<!DOCTYPE html>
<!-- NO data-theme here, since docs/gestaltung.md point 7. The default is the
     machine's own setting, and the stylesheet's prefers-color-scheme rule
     bites only while the attribute is absent. Writing "dark" here painted the
     first frame dark on a light machine and theme.js then repainted it - a
     flash in exactly the case that is now the normal one. color-scheme below
     still names both, so the browser's own widgets follow along. -->
<html lang="de">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="dark light">
<title>NGC</title>
<link rel="stylesheet" href="/assets/css/shell.css">
</head>
<body>
<div id="root"></div>
<noscript>NGC braucht JavaScript.</noscript>
<script type="module" src="/assets/js/shell/main.js"></script>
</body>
</html>
