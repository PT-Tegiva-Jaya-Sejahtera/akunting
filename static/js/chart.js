// chart.js: lapisan interaktif grafik dashboard (hover + tooltip).
// Grafik digambar server sebagai SVG; berkas ini hanya menambah penanda
// hover, garis bantu, dan kotak nilai. Tanpa pustaka luar.
(function () {
  var MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'Mei', 'Jun', 'Jul', 'Agu', 'Sep', 'Okt', 'Nov', 'Des'];

  function tipFor(wrap) {
    var tip = wrap.querySelector('.js-chart-tip');
    if (!tip) {
      tip = document.createElement('div');
      tip.className = 'js-chart-tip pointer-events-none absolute z-20 hidden w-max max-w-[240px] rounded-input border border-line bg-white px-3 py-2 text-xs shadow-dropdown';
      wrap.appendChild(tip);
    }
    return tip;
  }

  function show(wrap, tip, hit, clientX) {
    var index = hit.getAttribute('data-index');
    var wrapBox = wrap.getBoundingClientRect();
    var svg = wrap.querySelector('svg');
    var svgBox = svg.getBoundingClientRect();

    // Garis bantu di posisi titik aktif.
    var guide = wrap.querySelector('.js-guide-line');
    if (guide) {
      var x = parseFloat(hit.getAttribute('data-x') || '0');
      guide.setAttribute('x1', x);
      guide.setAttribute('x2', x);
      var layer = wrap.querySelector('.js-chart-guide');
      if (layer) layer.classList.remove('hidden');
    }
    // Titik data aktif diberi opasitas penuh.
    var dots = wrap.querySelectorAll('.js-chart-dot');
    for (var i = 0; i < dots.length; i++) {
      if (dots[i].getAttribute('data-dot') === index) dots[i].classList.remove('opacity-0');
      else dots[i].classList.add('opacity-0');
    }

    var label = hit.getAttribute('data-label') || '';
    var rev = hit.getAttribute('data-rev') || '';
    var exp = hit.getAttribute('data-exp') || '';
    var net = hit.getAttribute('data-net') || '';
    var rows = [];
    if (rev !== '') rows.push('<div class="flex items-center justify-between gap-4"><span class="text-muted">Pendapatan</span><span class="num font-medium text-ink">' + rev + '</span></div>');
    if (exp !== '') rows.push('<div class="flex items-center justify-between gap-4"><span class="text-muted">Beban</span><span class="num font-medium text-ink">' + exp + '</span></div>');
    var netCls = net.charAt(0) === '-' ? 'num font-semibold text-danger' : 'num font-semibold text-ink';
    if (net !== '') rows.push('<div class="mt-1 flex items-center justify-between gap-4 border-t border-line pt-1"><span class="text-muted">' + (wrap.getAttribute('data-chart') === 'bar' ? 'Laba (rugi)' : 'Selisih') + '</span><span class="' + netCls + '">' + net + '</span></div>');
    tip.innerHTML = '<div class="mb-1 font-semibold text-ink">' + label + '</div>' + rows.join('');

    var svgScale = svgBox.width / parseFloat(svg.getAttribute('viewBox').split(' ')[2] || svgBox.width);
    var hitX = parseFloat(hit.getAttribute('data-x') || '0') * svgScale;
    tip.classList.remove('hidden');
    var left = hitX - tip.offsetWidth / 2;
    var max = wrapBox.width - tip.offsetWidth - 4;
    if (left < 4) left = 4;
    if (left > max) left = max;
    tip.style.left = left + 'px';
    tip.style.top = Math.max(0, parseFloat(hit.getAttribute('data-y') || '0') * svgScale - tip.offsetHeight - 8) + 'px';
  }

  function hide(wrap) {
    var tip = wrap.querySelector('.js-chart-tip');
    if (tip) tip.classList.add('hidden');
    var layer = wrap.querySelector('.js-chart-guide');
    if (layer) layer.classList.add('hidden');
    var dots = wrap.querySelectorAll('.js-chart-dot');
    for (var i = 0; i < dots.length; i++) dots[i].classList.add('opacity-0');
  }

  function init(root) {
    var wraps = (root || document).querySelectorAll('.js-chart:not([data-ready])');
    for (var w = 0; w < wraps.length; w++) {
      (function (wrap) {
        wrap.setAttribute('data-ready', '1');
        var tip = tipFor(wrap);
        var hits = wrap.querySelectorAll('.js-chart-hit');
        for (var i = 0; i < hits.length; i++) {
          (function (hit) {
            hit.addEventListener('mouseenter', function (e) { show(wrap, tip, hit, e.clientX); });
            hit.addEventListener('mousemove', function (e) { show(wrap, tip, hit, e.clientX); });
          })(hits[i]);
        }
        wrap.addEventListener('mouseleave', function () { hide(wrap); });
        // Sentuh: ketuk sekali untuk menampilkan nilai.
        for (var j = 0; j < hits.length; j++) {
          hits[j].addEventListener('touchstart', function () { show(wrap, tip, this, 0); });
        }
      })(wraps[w]);
    }
  }

  document.addEventListener('DOMContentLoaded', function () { init(document); });
  document.addEventListener('htmx:afterSwap', function (e) { init(e.target || document); });
  if (document.readyState !== 'loading') init(document);
  window.chartMonths = MONTHS;
})();
