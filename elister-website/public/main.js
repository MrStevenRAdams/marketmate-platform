/* ============================================================
   MarketMate — Global JS
   ============================================================ */

(function () {
  'use strict';

  /* ── Nav scroll shadow ──────────────────────────────────── */
  const nav = document.getElementById('nav');
  if (nav) {
    window.addEventListener('scroll', () => {
      nav.classList.toggle('scrolled', window.scrollY > 8);
    }, { passive: true });
  }

  /* ── Mobile nav toggle ──────────────────────────────────── */
  const burger = document.getElementById('navBurger');
  const mobileMenu = document.getElementById('navMobile');
  if (burger && mobileMenu) {
    burger.addEventListener('click', () => {
      mobileMenu.classList.toggle('open');
      const isOpen = mobileMenu.classList.contains('open');
      burger.setAttribute('aria-expanded', isOpen);
      burger.querySelectorAll('span').forEach((s, i) => {
        s.style.transform = isOpen
          ? (i === 0 ? 'rotate(45deg) translate(5px, 5px)' : i === 2 ? 'rotate(-45deg) translate(5px, -5px)' : 'scaleX(0)')
          : '';
        s.style.opacity = (!isOpen || i !== 1) ? '' : '0';
      });
    });
  }

  /* ── FAQ accordion ──────────────────────────────────────── */
  document.querySelectorAll('.faq-question').forEach(btn => {
    btn.addEventListener('click', () => {
      const item = btn.closest('.faq-item');
      const isOpen = item.classList.contains('open');
      document.querySelectorAll('.faq-item.open').forEach(el => el.classList.remove('open'));
      if (!isOpen) item.classList.add('open');
    });
  });

  /* ── Smooth-scroll internal links ───────────────────────── */
  document.querySelectorAll('a[href^="#"]').forEach(a => {
    a.addEventListener('click', e => {
      const target = document.querySelector(a.getAttribute('href'));
      if (target) {
        e.preventDefault();
        target.scrollIntoView({ behavior: 'smooth', block: 'start' });
      }
    });
  });

  /* ── Active nav highlight ───────────────────────────────── */
  const path = window.location.pathname.split('/').pop() || 'index.html';
  document.querySelectorAll('.nav-links a').forEach(a => {
    if (a.getAttribute('href') === path) a.classList.add('active');
  });

  /* ── Contact form ───────────────────────────────────────── */
  const form = document.getElementById('contactForm');
  if (form) {
    form.addEventListener('submit', async e => {
      e.preventDefault();
      const btn = form.querySelector('button[type=submit]');
      const orig = btn.textContent;
      btn.textContent = 'Sending…';
      btn.disabled = true;
      try {
        const data = Object.fromEntries(new FormData(form));
        // In production, replace with your endpoint or EmailJS call
        await new Promise(r => setTimeout(r, 1000));
        const success = document.getElementById('formSuccess');
        if (success) {
          success.style.display = 'block';
          form.reset();
        }
      } catch {
        btn.textContent = 'Error — please try again';
      } finally {
        btn.textContent = orig;
        btn.disabled = false;
      }
    });
  }

  /* ── Scroll-reveal animation ─────────────────────────────── */
  if ('IntersectionObserver' in window) {
    const obs = new IntersectionObserver((entries) => {
      entries.forEach(e => {
        if (e.isIntersecting) {
          e.target.style.opacity = '1';
          e.target.style.transform = 'translateY(0)';
          obs.unobserve(e.target);
        }
      });
    }, { threshold: 0.12, rootMargin: '0px 0px -40px 0px' });

    document.querySelectorAll('.reveal').forEach(el => {
      el.style.opacity = '0';
      el.style.transform = 'translateY(24px)';
      el.style.transition = 'opacity .55s ease, transform .55s ease';
      obs.observe(el);
    });
  }

  /* ── Pricing toggle (monthly / annual) ──────────────────── */
  const toggle = document.getElementById('billingToggle');
  if (toggle) {
    toggle.addEventListener('change', () => {
      const annual = toggle.checked;
      document.querySelectorAll('.price-monthly').forEach(el => {
        el.style.display = annual ? 'none' : 'block';
      });
      document.querySelectorAll('.price-annual').forEach(el => {
        el.style.display = annual ? 'block' : 'none';
      });
      document.querySelectorAll('.billing-label').forEach(el => {
        el.textContent = annual ? 'per month, billed annually' : 'per month';
      });
    });
  }

})();
