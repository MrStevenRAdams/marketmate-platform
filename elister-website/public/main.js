// ─── e-Lister main.js ─────────────────────────────────────────────────────────

// ─── Mobile nav toggle ────────────────────────────────────────────────────────
(function () {
  const burger  = document.getElementById('navBurger');
  const mobile  = document.getElementById('navMobile');
  if (!burger || !mobile) return;

  burger.addEventListener('click', function () {
    const open = mobile.classList.toggle('open');
    burger.setAttribute('aria-expanded', open);
  });

  // Close on outside click
  document.addEventListener('click', function (e) {
    if (!burger.contains(e.target) && !mobile.contains(e.target)) {
      mobile.classList.remove('open');
    }
  });
})();

// ─── Sticky nav shadow on scroll ──────────────────────────────────────────────
(function () {
  const nav = document.getElementById('nav');
  if (!nav) return;
  window.addEventListener('scroll', function () {
    nav.style.boxShadow = window.scrollY > 20
      ? '0 4px 24px rgba(0,0,0,.4)'
      : '';
  }, { passive: true });
})();

// ─── Animate elements into view ───────────────────────────────────────────────
(function () {
  if (!('IntersectionObserver' in window)) return;
  const els = document.querySelectorAll(
    '.pillar-card, .step-card, .channel-tile, .channel-card, .pricing-card, .value-card, .feature-grid, .stat-item'
  );
  const obs = new IntersectionObserver(function (entries) {
    entries.forEach(function (entry) {
      if (entry.isIntersecting) {
        entry.target.style.opacity = '1';
        entry.target.style.transform = 'translateY(0)';
        obs.unobserve(entry.target);
      }
    });
  }, { threshold: 0.12 });

  els.forEach(function (el) {
    el.style.opacity = '0';
    el.style.transform = 'translateY(18px)';
    el.style.transition = 'opacity .45s ease, transform .45s ease';
    obs.observe(el);
  });
})();

// ─── Active nav link ──────────────────────────────────────────────────────────
(function () {
  const path = window.location.pathname.split('/').pop() || 'index.html';
  document.querySelectorAll('.nav-links a').forEach(function (a) {
    const href = a.getAttribute('href');
    if (href === path) a.classList.add('active');
  });
})();
