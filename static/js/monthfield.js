// monthfield.js: kolom pemilih bulan-tahun (mm/yyyy) dengan popup pilih bulan
// dan navigasi tahun. Kolom month bawaan browser punya tampilan berbeda dan
// tidak semua browser mendukungnya, jadi kontrolnya dibuat sendiri. Tanpa
// pustaka luar.
(function () {
  var MONTHS = ['Januari', 'Februari', 'Maret', 'April', 'Mei', 'Juni', 'Juli', 'Agustus', 'September', 'Oktober', 'November', 'Desember'];
  var MONTHS_SHORT = ['Jan', 'Feb', 'Mar', 'Apr', 'Mei', 'Jun', 'Jul', 'Ags', 'Sep', 'Okt', 'Nov', 'Des'];

  function parse(text) {
    var m = /^(\d{2})\/(\d{4})$/.exec((text || '').trim());
    if (!m) return null;
    var mo = parseInt(m[1], 10), y = parseInt(m[2], 10);
    if (mo < 1 || mo > 12 || y < 1) return null;
    return { month: mo, year: y };
  }

  function mask(text) {
    var digits = (text || '').replace(/\D/g, '').slice(0, 6);
    var out = digits.slice(0, 2);
    if (digits.length > 2) out += '/' + digits.slice(2, 6);
    return out;
  }

  // valueToMonth membaca nilai "2006-01" (dipakai value bawaan server).
  function valueToMonth(v) {
    var m = /^(\d{4})-(\d{2})$/.exec((v || '').trim());
    if (!m) return null;
    return { month: parseInt(m[2], 10), year: parseInt(m[1], 10) };
  }

  function panelHTML(view, selected) {
    var html = '<div class="flex items-center justify-between gap-2">' +
      '<button type="button" class="js-mo-prev rounded p-1 text-muted hover:bg-panel hover:text-ink" aria-label="Tahun sebelumnya">‹</button>' +
      '<div class="text-sm font-semibold text-ink">' + view.year + '</div>' +
      '<button type="button" class="js-mo-next rounded p-1 text-muted hover:bg-panel hover:text-ink" aria-label="Tahun berikutnya">›</button>' +
      '</div><div class="mt-2 grid grid-cols-3 gap-1 text-center text-xs">';
    for (var i = 1; i <= 12; i++) {
      var isSel = selected && selected.year === view.year && selected.month === i;
      var isToday = new Date().getFullYear() === view.year && new Date().getMonth() + 1 === i;
      var cls = 'js-mo-month rounded-[6px] py-2 transition-colors ';
      if (isSel) cls += 'bg-accent font-semibold text-white';
      else if (isToday) cls += 'bg-accent-wash font-medium text-accent';
      else cls += 'text-label hover:bg-panel hover:text-ink';
      html += '<button type="button" class="' + cls + '" data-month="' + i + '">' + MONTHS_SHORT[i - 1] + '</button>';
    }
    html += '</div><div class="mt-2 flex items-center justify-between border-t border-line pt-2">' +
      '<button type="button" class="js-mo-now text-xs font-medium text-accent hover:underline">Bulan ini</button>' +
      '<button type="button" class="js-mo-clear text-xs text-muted hover:text-ink">Kosongkan</button>' +
      '</div>';
    return html;
  }

  function init(root) {
    var fields = (root || document).querySelectorAll('.js-monthfield:not([data-ready])');
    for (var i = 0; i < fields.length; i++) {
      (function (field) {
        field.setAttribute('data-ready', '1');
        var input = field.querySelector('input');
        var panel = field.querySelector('.js-monthfield-panel');
        var error = field.querySelector('.js-monthfield-error');
        var toggle = field.querySelector('.js-monthfield-toggle');
        var view = parse(input.value) || valueToMonth(input.getAttribute('data-value') || '') ||
          { month: new Date().getMonth() + 1, year: new Date().getFullYear() };

        function setError(on) {
          if (error) error.classList.toggle('hidden', !on);
          input.classList.toggle('border-danger', on);
        }

        function close() { panel.classList.add('hidden'); }
        function open() {
          view = parse(input.value) || view;
          panel.innerHTML = panelHTML(view, parse(input.value));
          panel.classList.remove('hidden');
        }

        input.addEventListener('input', function () {
          var caretEnd = this.selectionStart >= this.value.length;
          this.value = mask(this.value);
          if (caretEnd) this.setSelectionRange(this.value.length, this.value.length);
          setError(this.value !== '' && !parse(this.value));
          if (panel && !panel.classList.contains('hidden')) {
            view = parse(this.value) || view;
            panel.innerHTML = panelHTML(view, parse(this.value));
          }
        });
        input.addEventListener('blur', function () {
          setError(this.value !== '' && !parse(this.value));
        });
        if (toggle) toggle.addEventListener('click', function () {
          if (panel.classList.contains('hidden')) open(); else close();
        });
        panel.addEventListener('click', function (e) {
          var monthBtn = e.target.closest('.js-mo-month');
          if (monthBtn) {
            var mo = parseInt(monthBtn.getAttribute('data-month'), 10);
            input.value = (mo < 10 ? '0' + mo : String(mo)) + '/' + view.year;
            setError(false);
            close();
            input.dispatchEvent(new Event('change', { bubbles: true }));
            return;
          }
          if (e.target.closest('.js-mo-prev')) { view = { month: view.month, year: view.year - 1 }; panel.innerHTML = panelHTML(view, parse(input.value)); return; }
          if (e.target.closest('.js-mo-next')) { view = { month: view.month, year: view.year + 1 }; panel.innerHTML = panelHTML(view, parse(input.value)); return; }
          if (e.target.closest('.js-mo-now')) {
            var t = new Date();
            input.value = pad2(t.getMonth() + 1) + '/' + t.getFullYear();
            setError(false); close();
            input.dispatchEvent(new Event('change', { bubbles: true }));
            return;
          }
          if (e.target.closest('.js-mo-clear')) { input.value = ''; setError(false); close(); input.dispatchEvent(new Event('change', { bubbles: true })); }
        });
        document.addEventListener('click', function (e) {
          if (!field.contains(e.target)) close();
        });
        document.addEventListener('keydown', function (e) {
          if (e.key === 'Escape') close();
        });
      })(fields[i]);
    }
  }

  function pad2(n) { return (n < 10 ? '0' : '') + n; }

  document.addEventListener('DOMContentLoaded', function () { init(document); });
  document.addEventListener('htmx:afterSwap', function (e) { init(e.target || document); });
  if (document.readyState !== 'loading') init(document);
})();
