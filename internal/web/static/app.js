// The dashboard's only script: it keeps the page current. The server renders every page; this fetches the same page
// again when the event stream says something happened (and every 15 seconds, because a container's state changes
// without any whalenurse event) and swaps in the new <main> and banners. Without JavaScript the pages still work.
(function () {
  'use strict';
  document.documentElement.classList.add('js'); // the copy buttons exist only for browsers that run this script
  var live = document.getElementById('live');
  var timer = null;
  var busy = false;

  function setLive(text, ok) {
    if (!live) { return; }
    live.textContent = text;
    live.className = ok ? 'live live-on' : 'live live-off';
  }

  function swap(doc, id) {
    var from = doc.getElementById(id);
    var to = document.getElementById(id);
    if (from && to) { to.replaceChildren.apply(to, Array.prototype.slice.call(document.importNode(from, true).childNodes)); }
  }

  function refresh() {
    if (busy || document.hidden) { return; }
    busy = true;
    fetch(location.pathname + location.search, { credentials: 'same-origin', cache: 'no-store' })
      .then(function (r) { if (!r.ok) { throw new Error('HTTP ' + r.status); } return r.text(); })
      .then(function (html) {
        var doc = new DOMParser().parseFromString(html, 'text/html');
        swap(doc, 'content');
        swap(doc, 'banners');
        setLive('live', true);
      })
      .catch(function () { setLive('offline', false); })
      .then(function () { busy = false; });
  }

  function soon() {
    if (timer) { return; }
    timer = setTimeout(function () { timer = null; refresh(); }, 700);
  }

  // One button per rollback command: copies the text of the <pre> beside it. The clipboard API needs a secure context
  // (https or localhost); on plain http a hidden textarea and execCommand do the same.
  function fallbackCopy(text) {
    var ta = document.createElement('textarea');
    ta.value = text;
    ta.setAttribute('readonly', '');
    ta.className = 'offscreen';
    document.body.appendChild(ta);
    ta.select();
    var ok = false;
    try { ok = document.execCommand('copy'); } catch (e) { ok = false; }
    document.body.removeChild(ta);
    return ok;
  }
  function copyText(text) {
    if (navigator.clipboard && window.isSecureContext) {
      return navigator.clipboard.writeText(text).then(function () { return true; }, function () { return fallbackCopy(text); });
    }
    return Promise.resolve(fallbackCopy(text));
  }
  document.addEventListener('click', function (ev) {
    var b = ev.target.closest && ev.target.closest('button[data-copy]');
    var pre = b && b.parentNode.querySelector('pre');
    if (!pre) { return; }
    copyText(pre.textContent.trim()).then(function (ok) {
      b.textContent = ok ? 'Copied' : 'Select and copy';
      setTimeout(function () { b.textContent = 'Copy'; }, 1500);
    });
  });

  setInterval(refresh, 15000);
  document.addEventListener('visibilitychange', refresh);

  if (window.EventSource) {
    var es = new EventSource('/api/v1/stream');
    es.onopen = function () { setLive('live', true); };
    es.onerror = function () { setLive('reconnecting', false); };
    es.onmessage = soon;
    // Named events are the domain event types; one listener per type would be long, so listen for the few kinds that
    // change what a page shows and let the 15 second refresh catch the rest.
    ['IncidentOpened', 'IncidentClosed', 'ActionResult', 'Suspended', 'Unsuspended', 'Recovered', 'Paused', 'Resumed',
      'DriftBlocked', 'DriftWarned', 'TagMoved', 'Upgraded', 'RateLimited', 'NotifyOnly', 'NeedsRecreate', 'RungsDisabled',
      'ManualStopMarked', 'ManualStopCleared', 'reset'].forEach(function (t) { es.addEventListener(t, soon); });
  } else {
    setLive('polling', true);
  }
})();
