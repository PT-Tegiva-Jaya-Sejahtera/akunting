// ui.js: perilaku kecil antarmuka. Popup akun dan picker perusahaan ditutup
// ketika pengguna mengklik di luar, seperti komponen akun umumnya.
(function () {
  document.addEventListener('click', function (e) {
    var menus = document.querySelectorAll('details.js-menu[open]');
    for (var i = 0; i < menus.length; i++) {
      if (!menus[i].contains(e.target)) menus[i].removeAttribute('open');
    }
  });
  document.addEventListener('keydown', function (e) {
    if (e.key !== 'Escape') return;
    var menus = document.querySelectorAll('details.js-menu[open]');
    for (var i = 0; i < menus.length; i++) menus[i].removeAttribute('open');
  });
  document.addEventListener('htmx:afterSwap', function (e) {
    var root = e.target || document;
    var tooltips = root.querySelectorAll ? root.querySelectorAll('.js-chart-tip') : [];
    for (var i = 0; i < tooltips.length; i++) tooltips[i].classList.add('hidden');
  });
})();
