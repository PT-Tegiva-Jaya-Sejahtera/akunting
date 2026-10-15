// datefield.js: kolom tanggal format Indonesia (dd/mm/yyyy) dengan kalender
// kecil. Input tanggal bawaan browser selalu memakai format lokal browser
// (MM/DD/YYYY pada en-US), jadi kolom diganti kontrol sendiri. Tanpa pustaka luar.
(function () {
  var MONTHS = ['Januari', 'Februari', 'Maret', 'April', 'Mei', 'Juni', 'Juli', 'Agustus', 'September', 'Oktober', 'November', 'Desember'];
  var DAYS = ['Sen', 'Sel', 'Rab', 'Kam', 'Jum', 'Sab', 'Min'];

  function pad(n) { return (n < 10 ? '0' : '') + n; }

  function parseID(text) {
    var m = /^(\d{2})\/(\d{2})\/(\d{4})$/.exec((text || '').trim());
    if (!m) return null;
    var d = parseInt(m[1], 10), mo = parseInt(m[2], 10), y = parseInt(m[3], 10);
    if (mo < 1 || mo > 12 || d < 1 || d > 31) return null;
    var dt = new Date(y, mo - 1, d);
    if (dt.getDate() !== d || dt.getMonth() !== mo - 1) return null;
    return dt;
  }

  function mask(text) {
    var digits = (text || '').replace(/\D/g, '').slice(0, 8);
    var out = digits.slice(0, 2);
    if (digits.length > 2) out += '/' + digits.slice(2, 4);
    if (digits.length > 4) out += '/' + digits.slice(4, 8);
    return out;
  }

  function calendarHTML(view, selected) {
    var today = new Date();
    var first = new Date(view.getFullYear(), view.getMonth(), 1);
    // Senin sebagai hari pertama.
    var offset = (first.getDay() + 6) % 7;
    var days = new Date(view.getFullYear(), view.getMonth() + 1, 0).getDate();
    var html = '<div class="flex items-center justify-between gap-2">' +
      '<button type="button" class="js-cal-prev rounded p-1 text-muted hover:bg-panel hover:text-ink" aria-label="Bulan sebelumnya">‹</button>' +
      '<div class="text-sm font-semibold text-ink">' + MONTHS[view.getMonth()] + ' ' + view.getFullYear() + '</div>' +
      '<button type="button" class="js-cal-next rounded p-1 text-muted hover:bg-panel hover:text-ink" aria-label="Bulan berikutnya">›</button>' +
      '</div><div class="mt-2 grid grid-cols-7 gap-0.5 text-center text-[10px] font-semibold uppercase text-muted">';
    for (var d = 0; d < 7; d++) html += '<span>' + DAYS[d] + '</span>';
    html += '</div><div class="mt-1 grid grid-cols-7 gap-0.5 text-center text-xs">';
    for (var i = 0; i < offset; i++) html += '<span></span>';
    for (var day = 1; day <= days; day++) {
      var isSel = selected && selected.getDate() === day && selected.getMonth() === view.getMonth() && selected.getFullYear() === view.getFullYear();
      var isToday = today.getDate() === day && today.getMonth() === view.getMonth() && today.getFullYear() === view.getFullYear();
      var cls = 'js-cal-day rounded-[6px] py-1.5 transition-colors ';
      if (isSel) cls += 'bg-accent font-semibold text-white';
      else if (isToday) cls += 'bg-accent-wash font-medium text-accent';
      else cls += 'text-label hover:bg-panel hover:text-ink';
      html += '<button type="button" class="' + cls + '" data-day="' + day + '">' + day + '</button>';
    }
    html += '</div><div class="mt-2 flex items-center justify-between border-t border-line pt-2">' +
      '<button type="button" class="js-cal-today text-xs font-medium text-accent hover:underline">Hari ini</button>' +
      '<button type="button" class="js-cal-clear text-xs text-muted hover:text-ink">Kosongkan</button>' +
      '</div>';
    return html;
  }

  function init(root) {
    var fields = (root || document).querySelectorAll('.js-datefield:not([data-ready])');
    for (var i = 0; i < fields.length; i++) {
      (function (field) {
        field.setAttribute('data-ready', '1');
        var input = field.querySelector('input');
        var panel = field.querySelector('.js-datefield-panel');
        var error = field.querySelector('.js-datefield-error');
        var toggle = field.querySelector('.js-datefield-toggle');
        var view = parseID(input.value) || new Date();

        function setError(on) {
          if (error) error.classList.toggle('hidden', !on);
          input.classList.toggle('border-danger', on);
        }

        function close() { panel.classList.add('hidden'); }
        function open() {
          view = parseID(input.value) || view || new Date();
          panel.innerHTML = calendarHTML(view, parseID(input.value));
          panel.classList.remove('hidden');
        }

        input.addEventListener('input', function () {
          var caretEnd = this.selectionStart >= this.value.length;
          this.value = mask(this.value);
          if (caretEnd) this.setSelectionRange(this.value.length, this.value.length);
          setError(this.value !== '' && !parseID(this.value));
          if (panel && !panel.classList.contains('hidden')) {
            view = parseID(this.value) || view;
            panel.innerHTML = calendarHTML(view, parseID(this.value));
          }
        });
        input.addEventListener('blur', function () {
          setError(this.value !== '' && !parseID(this.value));
        });
        if (toggle) toggle.addEventListener('click', function () {
          if (panel.classList.contains('hidden')) open(); else close();
        });
        panel.addEventListener('click', function (e) {
          var day = e.target.closest('.js-cal-day');
          if (day) {
            var dt = new Date(view.getFullYear(), view.getMonth(), parseInt(day.getAttribute('data-day'), 10));
            input.value = pad(dt.getDate()) + '/' + pad(dt.getMonth() + 1) + '/' + dt.getFullYear();
            setError(false);
            close();
            input.dispatchEvent(new Event('change', { bubbles: true }));
            return;
          }
          if (e.target.closest('.js-cal-prev')) { view = new Date(view.getFullYear(), view.getMonth() - 1, 1); panel.innerHTML = calendarHTML(view, parseID(input.value)); return; }
          if (e.target.closest('.js-cal-next')) { view = new Date(view.getFullYear(), view.getMonth() + 1, 1); panel.innerHTML = calendarHTML(view, parseID(input.value)); return; }
          if (e.target.closest('.js-cal-today')) {
            var t = new Date();
            input.value = pad(t.getDate()) + '/' + pad(t.getMonth() + 1) + '/' + t.getFullYear();
            setError(false); close();
            input.dispatchEvent(new Event('change', { bubbles: true }));
            return;
          }
          if (e.target.closest('.js-cal-clear')) { input.value = ''; setError(false); close(); input.dispatchEvent(new Event('change', { bubbles: true })); }
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

  document.addEventListener('DOMContentLoaded', function () { init(document); });
  document.addEventListener('htmx:afterSwap', function (e) { init(e.target || document); });
  if (document.readyState !== 'loading') init(document);
})();
