# e-Lister Website — Deployment Guide

## Overview

This is a **static website** (plain HTML/CSS/JS) with no server-side code. It is hosted on **Google Firebase Hosting** (free tier), which provides:
- HTTPS by default
- Global CDN
- Custom domain support
- Zero ongoing hosting costs for a low-traffic marketing site

Demo request emails are sent via **EmailJS**, which relays through your Gmail SMTP account. No backend server is required.

---

## Prerequisites

- A Google account (for Firebase)
- Node.js installed (https://nodejs.org — v18 or higher)
- An EmailJS account (free at https://www.emailjs.com)

---

## Step 1 — Set up EmailJS (for the demo request form)

EmailJS sends emails from the browser using your Gmail SMTP credentials without exposing them in your code.

### 1.1 Create an EmailJS account
1. Go to https://www.emailjs.com and sign up for a free account.
2. Verify your email address.

### 1.2 Connect your Gmail account
1. In the EmailJS dashboard, go to **Email Services** → **Add New Service**.
2. Select **Gmail**.
3. Click **Connect Account** and authorise with: **mrstevenradams@gmail.com** / **Moscow007@**
   *(Change the Gmail password after you have confirmed everything is working.)*
4. Give the service a name (e.g. `e-lister-gmail`). Note the **Service ID** — it looks like `service_xxxxxxx`. - service_a1wgjyu

### 1.3 Create an email template
1. Go to **Email Templates** → **Create New Template**.
2. Set **To Email** to: `{{to_email}}`
3. Set **Subject** to: `{{subject}}`
4. In the **Body** section, paste the following HTML template:

```html
<h2>New Demo Request — e-Lister</h2>
<p><strong>Submitted:</strong> {{submitted_at}}</p>

<hr/>
<h3>Contact Details</h3>
<p><strong>Name:</strong> {{first_name}} {{last_name}}</p>
<p><strong>Email:</strong> {{email}}</p>
<p><strong>Phone:</strong> {{phone}}</p>
<p><strong>Company:</strong> {{company}}</p>
<p><strong>Website:</strong> {{website}}</p>
<p><strong>Preferred demo time:</strong> {{preferred_time}}</p>

<hr/>
<h3>Business Profile</h3>
<p><strong>Business type:</strong> {{business_type}}</p>
<p><strong>Product category:</strong> {{product_category}}</p>
<p><strong>Active SKUs:</strong> {{sku_count}}</p>
<p><strong>Monthly orders:</strong> {{monthly_orders}}</p>
<p><strong>Current channels:</strong> {{current_channels}}</p>
<p><strong>Channels wanted:</strong> {{wanted_channels}}</p>
<p><strong>Fulfilment model:</strong> {{fulfilment_model}}</p>
<p><strong>Current software:</strong> {{current_software}}</p>
<p><strong>Top priorities:</strong> {{priorities}}</p>

<hr/>
<h3>Additional Notes</h3>
<p>{{additional_notes}}</p>
```

5. Save the template. Note the **Template ID** — it looks like `template_xxxxxxx`.

### 1.4 Get your Public Key
1. In the EmailJS dashboard, go to **Account** → **General**.
2. Copy your **Public Key**.

### 1.5 Add your keys to the website
Open the file `public/book-demo.html` in a text editor.

Find these three lines near the bottom of the file (inside the `<script>` tag):

```javascript
const EMAILJS_PUBLIC_KEY  = window.EMAILJS_PUBLIC_KEY  || 'YOUR_EMAILJS_PUBLIC_KEY';
const EMAILJS_SERVICE_ID  = window.EMAILJS_SERVICE_ID  || 'YOUR_EMAILJS_SERVICE_ID';
const EMAILJS_TEMPLATE_ID = window.EMAILJS_TEMPLATE_ID || 'YOUR_EMAILJS_TEMPLATE_ID';
```

Replace the placeholder strings with your actual values:

```javascript
const EMAILJS_PUBLIC_KEY  = window.EMAILJS_PUBLIC_KEY  || 'abc123yourpublickey';
const EMAILJS_SERVICE_ID  = window.EMAILJS_SERVICE_ID  || 'service_xxxxxxx';
const EMAILJS_TEMPLATE_ID = window.EMAILJS_TEMPLATE_ID || 'template_xxxxxxx';
```

Save the file.

---

## Step 2 — Install Firebase CLI

Open a terminal and run:

```bash
npm install -g firebase-tools
```

---

## Step 3 — Log in to Firebase

```bash
firebase login
```

This opens a browser window. Log in with your Google account.

---

## Step 4 — Create a Firebase project

1. Go to https://console.firebase.google.com
2. Click **Add project**
3. Name it `e-lister-website` (or similar)
4. Disable Google Analytics if you prefer (not needed for static hosting)
5. Click **Create project**

---

## Step 5 — Initialise Firebase in the project folder

In your terminal, navigate to the `elister-website` folder (the folder containing the `public/` directory):

```bash
cd /path/to/elister-website
firebase init hosting
```

When prompted:
- **Select a project**: choose the project you just created
- **What do you want to use as your public directory?**: type `public`
- **Configure as a single-page app?**: type `N` (No)
- **Set up automatic builds with GitHub?**: type `N` (No)
- **File public/index.html already exists. Overwrite?**: type `N` (No)

This creates a `firebase.json` file in the project root.

---

## Step 6 — Deploy to Firebase Hosting

```bash
firebase deploy --only hosting
```

Firebase will output a URL like: `https://e-lister-website-XXXXX.web.app`

Your site is now live. Visit the URL to check it works.

---

## Step 7 — Connect a custom domain

If you have a domain (e.g. `e-lister.io`):

1. In Firebase console, go to **Hosting** → **Add custom domain**
2. Enter your domain (e.g. `www.e-lister.io`)
3. Firebase will give you DNS records to add to your domain registrar
4. Add the records in your domain registrar's DNS settings
5. Wait for DNS propagation (usually 30 minutes to a few hours)
6. Firebase will automatically provision an SSL certificate

---

## Step 8 — Test the demo form

1. Open `book-demo.html` on your live site
2. Fill in the form and submit it
3. Check mrstevenradams@gmail.com for the email
4. If you don't receive it within a minute, check:
   - Your EmailJS dashboard → **Email Logs** for errors
   - Your Gmail spam folder
   - That your public key, service ID and template ID are all correct in `book-demo.html`

---

## Re-deploying after changes

Whenever you update any HTML, CSS or JS files, re-deploy with:

```bash
firebase deploy --only hosting
```

---

## Security notes

1. **Change your Gmail password** immediately after confirming the email form works.
2. The EmailJS public key is safe to expose in client-side code — it only allows sending via templates you have configured.
3. EmailJS's free plan allows 200 emails/month. If you receive more than 200 demo requests per month, upgrade to a paid plan.
4. EmailJS has spam protection built in. For additional protection, consider adding a honeypot field or integrating Google reCAPTCHA v3.

---

## File structure

```
elister-website/
├── public/
│   ├── index.html          ← Homepage
│   ├── features.html       ← Features page
│   ├── channels.html       ← Channels page
│   ├── pricing.html        ← Pricing page
│   ├── about.html          ← About page
│   ├── book-demo.html      ← Demo booking form (sends email)
│   ├── privacy.html        ← Privacy Policy
│   ├── terms.html          ← Terms of Service
│   ├── cookies.html        ← Cookie Policy
│   ├── styles.css          ← All styles
│   └── main.js             ← JavaScript (nav, animations)
├── firebase.json           ← Created by firebase init
├── .firebaserc             ← Created by firebase init
└── DEPLOYMENT.md           ← This file
```

---

## Customisation checklist before going live

- [ ] Replace `[Company Name]` in `privacy.html` and `terms.html` with your actual registered company name
- [ ] Add your registered company address and company number to the footer of both legal pages
- [ ] Replace `privacy@e-lister.io` and `legal@e-lister.io` in the legal pages with your actual contact emails (or use the same address for both)
- [ ] Replace `hello@e-lister.io` in the form error fallback with a real contact address
- [ ] Add EmailJS keys as described in Step 1.5
- [ ] Update the `og:image` meta tag on each page once you have a logo or OG image
- [ ] Register your canonical domain and update `rel="canonical"` URLs across all pages
- [ ] Change Gmail password after confirming email delivery works
