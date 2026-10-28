// Behavioural feature collection for the challenge page only.
// Everything stays in this page: no cookies, no storage, no identifiers.
// The feature vector is posted with the form as `f` and is evaluated (shadow
// mode for now) by the server.
(function () {
  "use strict";

  var t0 = performance.now();
  var firstMove = null, moves = 0, path = 0, last = null, firstPt = null;
  var speeds = [], lastSpeed = null, accelSigns = 0, coalescedMax = 0;
  var pressureSeen = false, firstKey = null;
  var downAt = {}, dwells = [], gaps = [], lastUp = null;
  var edits = 0, pasted = false, visChanges = 0;
  var clickDown = null, clickMs = 0, offClicks = 0;

  function now() { return performance.now(); }

  window.addEventListener("pointermove", function (e) {
    if (firstMove === null) firstMove = now() - t0;
    moves++;
    if (!firstPt) firstPt = { x: e.clientX, y: e.clientY };
    var ce = 0;
    try { if (e.getCoalescedEvents) ce = e.getCoalescedEvents().length; } catch (err) { ce = 0; }
    if (ce > coalescedMax) coalescedMax = ce;
    if ((e.pressure && e.pressure > 0) || (e.width && e.width > 0)) pressureSeen = true;
    if (last) {
      var dx = e.clientX - last.x, dy = e.clientY - last.y;
      var d = Math.sqrt(dx * dx + dy * dy), dt = now() - last.t;
      if (dt > 0 && dt < 500) {
        path += d;
        var v = (d / dt) * 1000;
        speeds.push(v);
        if (lastSpeed !== null) {
          var acc = v - lastSpeed;
          if (lastSpeed !== 0 && (acc > 0) !== (lastSpeed > 0)) accelSigns++;
        }
        lastSpeed = v;
      }
    }
    last = { x: e.clientX, y: e.clientY, t: now() };
  }, { passive: true, capture: true });

  window.addEventListener("keydown", function (e) {
    if (firstKey === null) firstKey = now() - t0;
    if (!downAt[e.code]) downAt[e.code] = now();
    if (e.key === "Backspace" || e.key === "Delete") edits++;
  }, { passive: true, capture: true });

  window.addEventListener("keyup", function (e) {
    var d = downAt[e.code];
    if (d) {
      dwells.push(now() - d);
      delete downAt[e.code];
    }
    var up = now();
    if (lastUp !== null) gaps.push(up - lastUp);
    lastUp = up;
  }, { passive: true, capture: true });

  window.addEventListener("paste", function () { pasted = true; }, { passive: true, capture: true });

  window.addEventListener("pointerdown", function (e) {
    if (e.target && e.target.closest && e.target.closest("form")) clickDown = now();
    else offClicks++;
  }, { passive: true, capture: true });

  window.addEventListener("pointerup", function () {
    if (clickDown !== null) { clickMs = now() - clickDown; clickDown = null; }
  }, { passive: true, capture: true });

  document.addEventListener("visibilitychange", function () { visChanges++; });

  function pct(arr, p) {
    if (!arr.length) return 0;
    var a = arr.slice().sort(function (x, y) { return x - y; });
    var i = Math.min(a.length - 1, Math.max(0, Math.round((a.length - 1) * p)));
    return a[i];
  }
  function median(arr) { return pct(arr, 0.5); }
  function stdev(arr) {
    if (arr.length < 2) return 0;
    var m = arr.reduce(function (a, b) { return a + b; }, 0) / arr.length;
    var v = arr.reduce(function (a, b) { return a + (b - m) * (b - m); }, 0) / (arr.length - 1);
    return Math.sqrt(v);
  }
  function round(n) { return Math.round(n * 10) / 10; }

  function features() {
    var straight = 0;
    if (firstPt && last) {
      var dx = last.x - firstPt.x, dy = last.y - firstPt.y;
      straight = Math.sqrt(dx * dx + dy * dy);
    }
    var eff = path > 0 ? Math.min(1, straight / path) : 0;
    return {
      t0_first_move: firstMove === null ? null : round(firstMove),
      n_moves: moves,
      path_eff: round(eff * 1000) / 1000,
      v_peak: round(pct(speeds, 1)),
      v_p95: round(pct(speeds, 0.95)),
      accel_signs: accelSigns,
      coalesced_max: coalescedMax,
      pressure_seen: pressureSeen,
      t_first_key: firstKey === null ? null : round(firstKey),
      dwell_med: round(median(dwells)),
      flight_std: round(stdev(gaps)),
      n_edits: edits,
      pasted: pasted,
      click_ms: round(clickMs),
      clicks_off: offClicks,
      vis_changes: visChanges,
      dwell_page: round(now() - t0),
      webdriver: navigator.webdriver === true,
      plugins: navigator.plugins ? navigator.plugins.length : 0,
      fonts: 0
    };
  }

  // fill the hidden field right before the browser serialises the form
  document.addEventListener("submit", function (e) {
    var form = e.target;
    if (!form || !form.querySelector) return;
    var hidden = form.querySelector('input[name="f"]');
    var tok = form.querySelector('input[name="d"]');
    if (!hidden) return;
    try {
      hidden.value = JSON.stringify({ d: tok ? tok.value : "", t: Date.now(), e: features() });
    } catch (err) { hidden.value = ""; }
  }, true);
})();
