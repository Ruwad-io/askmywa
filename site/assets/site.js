// Copy buttons on the terminal blocks; the page works without them.
document.querySelectorAll('.term').forEach(function (pre) {
  if (!navigator.clipboard) return;
  var code = pre.querySelector('code');
  var label = document.documentElement.lang === 'fr' ? 'Copier' : 'Copy';
  var b = document.createElement('button');
  b.className = 'copy';
  b.type = 'button';
  b.textContent = label;
  b.setAttribute('aria-label', label);
  b.addEventListener('click', function () {
    navigator.clipboard.writeText(code.textContent.trim()).then(function () {
      b.textContent = '✓';
      setTimeout(function () { b.textContent = label; }, 1500);
    });
  });
  pre.appendChild(b);
});
