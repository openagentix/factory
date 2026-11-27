// Sets the remembered theme before the page paints, so there is no flash.
try {
    var t = localStorage.getItem('oxf-theme');
    if (t === 'light' || t === 'dark') document.documentElement.setAttribute('data-theme', t);
} catch (e) {}
