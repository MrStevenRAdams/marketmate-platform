import { useState, useEffect } from 'react';

// ─── Types ────────────────────────────────────────────────────────────────────

interface DocSection {
  id: string;
  title: string;
  icon: string;
  content: DocPage[];
}

interface DocPage {
  id: string;
  title: string;
  body: string; // markdown-lite: supports ## headings, **bold**, `code`, bullet lists
}

// ─── Documentation Content ────────────────────────────────────────────────────

const DOCS: DocSection[] = [

  // ══════════════════════════════════════════════════════════════════════════
  // GETTING STARTED
  // ══════════════════════════════════════════════════════════════════════════
  {
    id: 'getting-started',
    title: 'Getting Started',
    icon: '🚀',
    content: [
      {
        id: 'platform-overview',
        title: 'Platform Overview',
        body: `
## What is MarketMate?

MarketMate is a multi-channel e-commerce operations platform that connects your product catalogue to marketplaces, manages inventory across multiple locations, and automates order fulfilment.

## Technology Stack

The backend is a Go service built on the Gin framework, backed by Google Cloud Firestore for data storage, Google Cloud Storage for files and label PDFs, and Google Cloud Tasks for asynchronous job processing. The frontend is a React/TypeScript single-page application hosted on Firebase.

Authentication uses Firebase Authentication. All API requests carry a tenant identifier so that each customer's data is fully isolated within Firestore.

## Capability Areas

**Product Information Management (PIM):** Central catalogue of Products, Variants, Attributes, and Categories. All channel listings are derived from this master record.

**Marketplace Listings:** Push products to Amazon, eBay, Etsy, BigCommerce, Bluepark, Back Market, TikTok Shop, Shopify, WooCommerce, Walmart, Kaufland, Magento, OnBuy, and Mirakl-powered channels. Each channel has its own handler and credential store.

**Order Management:** Orders from all connected channels flow into a unified inbox. Supports holds, locks, tags, notes, manual creation, merge, split, and cancellation.

**Inventory:** Multi-location stock tracking with bin and rack management, stock counts, scrapping, transfers, demand forecasting, and automated reorder suggestions.

**Dispatch:** Carrier rate shopping, label generation, and end-of-day manifests for DPD, Evri, Royal Mail, and FedEx.

**Returns:** Full RMA lifecycle from authorisation through inspection, restock, and refund push.

**Automation:** Event-driven and scheduled rule engine plus a Workflow engine for multi-step order processing.

**Analytics:** Overview dashboards, per-channel P&L, pivot analytics, and a report builder.

**AI:** Listing content generation powered by Gemini and/or Claude. Supports single-product, bulk, and channel-specific generation with schema-aware output.
`,
      },
      {
        id: 'navigating-the-app',
        title: 'Navigating the App',
        body: `
## Sidebar Navigation

The left sidebar is divided into collapsible sections. Click a section heading to expand or collapse it.

**CATALOG** — Products, Categories, Attributes. Your master product data lives here.

**MARKETPLACE** — Import (pull products from a marketplace), Listings (manage what is pushed to each channel).

**OPERATIONS** — Orders, Messages (buyer helpdesk), Import/Export, Purchase Orders, Suppliers, Vendor Orders.

**DISPATCH** — Dispatch console, Pickwaves, Label Printing, Manifests, SLA Dashboard, Delivery Exceptions.

**INVENTORY** — My Inventory, FBA Inbound, Warehouse locations, Storage Groups, Stock Count, Stock In, Stock Scrap, Warehouse Transfers, Picking Replenishment, Forecasting, Replenishment.

**RETURNS** — RMAs (return merchandise authorisations).

**AUTOMATION** — Workflows, Automation Rules, Workflow Simulator, Automation Logs.

**ANALYTICS** — Overview, Inventory Dashboard, Order Dashboard, Pivot Analytics, Reporting, Operational Dashboard.

## Global UI Elements

- **Notification bell** — in-app system notifications (stock alerts, job completions).
- **Sync Status panel** — slide-out panel showing processing tasks, pending jobs, and errors over the last 24 hours.
- **Changelog** — What's New panel accessible from the sidebar footer.
- **Help drawer** — the panel you are reading now. Use the search box at the top to find topics by keyword.
- **Tenant switcher** — if you manage multiple accounts, switch between them from the sidebar footer.

## Using Help Search

Type any keyword into the search box at the top of this drawer. Results are matched against page titles and body text across all sections.
`,
      },
      {
        id: 'concepts',
        title: 'Core Concepts',
        body: `
## Product

**Product** is the master record in the catalogue. It holds the canonical title, description, images, price, and stock level. Every listing on every marketplace is derived from a Product.

## Variant / SKU

**Variant** is a specific purchasable version of a Product — for example, a T-shirt in size Large, colour Blue. Each Variant has its own **SKU** (stock-keeping unit), price, and stock level. Variants are generated from Options (e.g. Size, Colour) and their Option Values.

## Listing

**Listing** is the representation of a Product (or Variant) on a specific marketplace Channel. A Listing exists in MarketMate's database and can be in a draft, published, or error state. Pushing a Listing sends it live to the channel.

## Channel

**Channel** is a marketplace connection — for example, an Amazon Seller Central account or an eBay store. Each Channel is identified by a stored Credential record per Tenant.

## Tenant

**Tenant** is a customer account within the multi-tenant platform. All Firestore data is scoped under **tenants/{tenant_id}/** so one tenant's data is never visible to another.

## Inventory Item

**Inventory Item** represents stock for a SKU across one or more warehouse locations. Each location record holds on-hand, reserved, available, inbound, and safety-stock quantities. Available = on-hand minus reserved.

## Reservation

**Reservation** is a hold placed on stock when an order is received, reducing the available quantity to prevent overselling before the item is physically dispatched.

## Automation Rule

**Automation Rule** is a condition/action rule evaluated by the Rule Engine. Rules can be triggered by events (such as a new order arriving) or run on a cron schedule. Scripts are pre-validated before saving.

## Import Job

**Import Job** tracks the progress of an async data import. Statuses are pending**, **processing**, **done**, and **failed. Jobs are visible in the import history UI.

## Fulfilment Source

**Fulfilment Source** is a warehouse, 3PL, or FBA location that holds stock and from which orders are dispatched. Each shipment records its Fulfilment Source.

## Shipment / Manifest

**Shipment** is a carrier booking for one or more orders. It stores the tracking number, label PDF, and marketplace reporting records. A **Manifest** is the end-of-day collection of shipments submitted to the carrier's close-out API.
`,
      },
    ],
  },

  // ══════════════════════════════════════════════════════════════════════════
  // CATALOG
  // ══════════════════════════════════════════════════════════════════════════
  {
    id: 'catalog',
    title: 'Catalog',
    icon: '📦',
    content: [
      {
        id: 'products',
        title: 'Products',
        body: `
## What is a Product?

A Product is the master record for an item you sell. It stores the title, description, images, brand, pricing, and stock level. All marketplace Listings are generated from this central record, so updating a Product can propagate changes to every channel.

## Creating a Product

1. Navigate to **Catalog → Products** and click **New Product**.
2. Enter the product title, description, and category.
3. Add images by uploading files via the image uploader (stored in Google Cloud Storage).
4. Set the base price and stock level.
5. Add any Attributes relevant to the product (see the Attributes page).
6. Click **Save** to create the Product.

## Editing a Product

Open the product from the list, make changes on any tab, and click **Save**. Changes are persisted immediately to Firestore.

## Duplicating a Product

From the product detail page, use the **Duplicate** action. A copy is created with all fields, variants, and attributes carried over. The duplicate is saved as a new record and does not affect the original.

## Bulk Operations

Products can be created or updated in bulk via the Import/Export section. Download the CSV template from **Operations → Import/Export → Templates**, populate it, and upload it through the import flow. See the Import/Export page for step-by-step instructions.

## Deleting a Product

Deletion is permanent. Ensure all associated Listings have been removed from marketplaces before deleting the Product, as orphaned listings on channels cannot be managed from MarketMate once the Product record is gone.
`,
      },
      {
        id: 'variants-and-skus',
        title: 'Variants and SKUs',
        body: `
## What is a Variant?

A Variant is a specific, purchasable version of a Product. For example, if a Product is a T-shirt, its Variants might be Small/Red and Large/Blue. Each Variant has its own SKU, price, stock level, and optionally its own image.

## Options and Option Values

To generate Variants, first define Options on the Product (e.g. Size, Colour) and add Option Values for each (e.g. Small, Medium, Large). MarketMate can then auto-generate all combinations as Variants.

## Variant Fields

Each Variant record holds: SKU, price, compare-at price, cost price, weight, dimensions, barcode/EAN, stock level, and an optional variant image.

## Managing Variants

From the Product detail page, open the **Variants** tab to:

- Add a single variant manually.
- Use **Generate Variants** to create all combinations from current Options.
- Edit individual variant fields inline.
- Delete variants that are no longer sold.

## SKU Uniqueness

SKUs must be unique across your entire catalogue. Use the SKU check endpoint (accessible via the Listings UI) to verify a SKU does not already exist before creating a new Variant.

## Inventory Tracking

Each Variant's stock is tracked through Inventory Items. When an order is received, a Reservation is placed against the SKU, reducing available stock. See the Inventory section for more detail.
`,
      },
      {
        id: 'attributes',
        title: 'Attributes',
        body: `
## What are Attributes?

Attributes are structured data fields that extend a Product beyond its core fields. They are used to store marketplace-specific requirements (such as Amazon product type fields) and product specifications (such as material, voltage, or country of origin).

## Attribute Sets

Attributes are grouped into **Attribute Sets**. You assign an Attribute Set to a Product, which gives that Product access to all Attributes in the set. This lets you maintain consistent field structures across a category of products.

## Creating an Attribute

1. Go to **Catalog → Attributes**.
2. Click **New Attribute**.
3. Set a name, data type (text, number, boolean, list), and optionally a list of allowed values.
4. Save the Attribute.

## Creating an Attribute Set

1. Click **New Attribute Set**.
2. Give the set a name (e.g. "Electronics" or "Amazon Clothing").
3. Add one or more Attributes to the set.
4. Save the set.

## Assigning Attributes to a Product

Open a Product, go to the **Attributes** tab, and assign an Attribute Set. You can then fill in values for each Attribute in that set.

## Use in Marketplace Listings

When generating a listing for a channel that requires structured data (such as Amazon's product type schema), MarketMate maps Attribute values into the channel's required fields. Keeping Attributes accurate reduces listing validation errors.
`,
      },
      {
        id: 'categories',
        title: 'Categories',
        body: `
## What are Categories?

Categories are a hierarchical taxonomy for organising your Products within MarketMate. They are separate from marketplace-specific category trees (such as eBay's category tree or Amazon's product types), which are managed per channel.

## Category Tree

Categories support a tree structure with parent and child relationships. Navigate to **Catalog → Categories** to view and manage the tree.

## Creating a Category

Click **New Category**, enter a name, and optionally select a parent category. Save to add it to the tree.

## Assigning Products to a Category

On the Product detail page, select a category from the category picker. A Product can belong to one internal category.

## Note on Category Handler

The **category_handler.go.disabled** file in the backend is **not currently active**. Full category management is handled through the product handler's category endpoints (**/api/v1/categories**), which are live and in use.
`,
      },
    ],
  },

  // ══════════════════════════════════════════════════════════════════════════
  // MARKETPLACE
  // ══════════════════════════════════════════════════════════════════════════
  {
    id: 'marketplace',
    title: 'Marketplace',
    icon: '🛍️',
    content: [
      {
        id: 'marketplace-overview',
        title: 'Marketplace Overview',
        body: `
## Supported Channels

The following channels are registered and active in the platform:

| Channel | Auth Method | Listings | Orders | Schema / Category Lookup |
| --- | --- | --- | --- | --- |
| Amazon | LWA OAuth + AWS IAM | ✓ | ✓ | Product type schema download |
| eBay | OAuth 2.0 | ✓ | ✓ | Category aspects + schema refresh |
| Etsy | OAuth 2.0 | ✓ | ✓ | Taxonomy properties |
| BigCommerce | API key (store_hash + access_token) | ✓ | ✓ | Category tree |
| Back Market | API key | ✓ | ✓ | — |
| Bluepark | API key | ✓ | ✓ | — |
| TikTok Shop | OAuth 2.0 | ✓ | ✓ | Category attributes |
| Shopify | API key | ✓ | ✓ | — |
| WooCommerce | Consumer key/secret | ✓ | ✓ | Category/attribute fetch |
| Walmart | Client ID + Secret | ✓ | ✓ | — |
| Kaufland | Client key + Secret | ✓ | ✓ | Category tree |
| Magento 2 | Integration token | ✓ | ✓ | Category tree |
| OnBuy | API key | ✓ | ✓ | Category tree |
| Mirakl channels | API key + instance URL | ✓ | ✓ | Category tree |

Mirakl-powered channels include: Tesco, B&Q, Superdrug, Debenhams, Decathlon, Mountain Warehouse, JD Sports, Carrefour, Fnac Darty, Leroy Merlin, MediaMarkt, ASOS, Macy's, and Lowe's.

## Channel Registry

The **Marketplace → Connections** page shows the dynamic channel registry. An admin can enable, disable, and reorder channels from the admin console without a code deployment.
`,
      },
      {
        id: 'connecting-a-channel',
        title: 'Connecting a Channel',
        body: `
## Adding a Channel Credential

1. Go to **Marketplace → Connections** and click **Connect** next to the channel you want to add.
2. For OAuth channels (Amazon, eBay, Etsy, TikTok Shop), you will be redirected to the channel's authorisation page. Complete the OAuth flow and you will be returned to MarketMate automatically.
3. For API key channels (BigCommerce, WooCommerce, Walmart, Kaufland, Magento, OnBuy, Back Market, Bluepark), enter the required credentials in the connection form and click **Test Connection** before saving.

## Credentials Required per Channel

**Amazon:** LWA Client ID, LWA Client Secret, LWA Refresh Token, AWS Access Key ID, AWS Secret Access Key, AWS Region, SP-API Endpoint, and Marketplace ID.

**eBay:** Authorised via OAuth. Requires the platform's eBay Developer App credentials to be configured (Client ID, Client Secret, Dev ID, RuName) — set once by the platform administrator.

**BigCommerce:** Store hash, Client ID, and Access Token.

**WooCommerce:** Store URL, Consumer Key, and Consumer Secret.

**Back Market / Bluepark:** API key.

## Credential Encryption

All channel credentials are encrypted at rest using AES-256 before being stored in Firestore. The encryption key is set via the **CREDENTIAL_ENCRYPTION_KEY** environment variable.

## Multiple Accounts per Channel

You can connect more than one account for the same channel — for example, two separate Amazon Seller Central accounts. Each account is stored as a separate Credential record. When triggering an import or listing, select the Credential you want to use.

## Testing a Connection

At any time, open a Credential and click **Test Connection** to verify that the stored credentials are still valid with the channel's API.
`,
      },
      {
        id: 'import-products',
        title: 'Importing Products from a Marketplace',
        body: `
## What is Marketplace Import?

Marketplace import pulls your existing product listings from a connected channel and creates Product records in MarketMate. It is the fastest way to populate your catalogue if you already have products listed on Amazon, eBay, or another channel.

## Starting an Import

1. Go to **Marketplace → Import**.
2. Select the channel and the Credential (account) to import from.
3. Click **Start Import**.

For Amazon, the import downloads your full merchant listings report from SP-API (typically taking 30–60 seconds), then enriches each product with catalogue data.

## Monitoring Progress

The import job progress is shown in real time: **0 / total** products processed. Once complete, the job shows counts of **Successful**, **Failed**, and **Updated** items.

- **Successful** — products fully imported and saved.
- **Failed** — products where enrichment or save failed. Click the red pill button to view the error log per product.
- **Updated** — products that already existed and were refreshed.

## Import Job Statuses

| Status | Meaning |
| --- | --- |
| **pending** | Job queued, not yet started |
| **processing** | Import is actively running |
| **done** | Import completed (may have partial failures) |
| **failed** | Import could not complete — check the error log |

## After Import

Imported products appear in **Catalog → Products** and can be pushed as listings to any other connected channel.
`,
      },
      {
        id: 'listings',
        title: 'Listings',
        body: `
## What is a Listing?

A Listing is the representation of a Product on a specific marketplace channel. A Product can have multiple Listings — one per channel or per account. The Listing record holds channel-specific fields, the current status on that channel, and any validation errors.

## Listing Lifecycle

1. **Draft** — the Listing exists in MarketMate but has not been pushed to the channel.
2. **Validated** — the Listing has passed local validation (required fields present, schema compliant).
3. **Published** — the Listing has been submitted to the channel's API and is live.
4. **Error** — the channel rejected the submission; the error message is stored on the Listing.

## Creating a Listing

1. Go to **Marketplace → Listings → New Listing**.
2. Select the channel and Credential.
3. Choose the Product to list.
4. Fill in any channel-specific required fields (these vary by channel schema).
5. Click **Validate** to check for errors before publishing.
6. Click **Publish** to submit to the channel.

## Bulk Publish

Select multiple Listings from the list view and use **Bulk Publish** to submit them all in one action. Errors per listing are shown after the operation.

## Listing Analytics

For channels that return impression and sales data, the **Analytics** tab on a Listing shows performance metrics. This is backed by **listing_analytics_handler.go**.

## Unlisted Products

The **Unlisted** view shows Products in your catalogue that do not yet have a Listing for the currently selected channel, helping you identify gaps.
`,
      },
      {
        id: 'amazon',
        title: 'Amazon',
        body: `
## Authentication

Amazon requires two sets of credentials: **LWA (Login with Amazon)** for SP-API access, and **AWS IAM** keys for request signing. Both must be configured per channel credential.

## Product Type Schema

Amazon's Listings Items API requires submissions to conform to a product type schema. MarketMate downloads and caches these schemas so you can map your Product Attributes to the correct Amazon fields.

1. Go to **Marketplace → Amazon → Schemas**.
2. Search for your product type (e.g. LUGGAGE**, **SHIRT).
3. Download the schema. MarketMate stores it and uses it to validate listings before submission.
4. Schemas can be set to auto-refresh on a configurable interval.

## Creating and Submitting a Listing

1. Use **Prepare** to map Product data to the Amazon schema fields.
2. Use **Validate** to run Amazon's local validation rules.
3. Use **Submit** to call Amazon's Listings Items API.

## Orders

Amazon orders are downloaded via SP-API. Use the **Download Now** button in Operations → Orders or enable automatic polling via the order sync settings.

## FBA

Amazon FBA inbound shipments are managed under **Inventory → FBA Inbound**. Create, plan, and confirm inbound shipment plans from there.

## Restrictions

The **Restrictions** check endpoint queries Amazon to determine whether your account can list a specific ASIN, and returns the reason if listing is blocked.
`,
      },
      {
        id: 'ebay',
        title: 'eBay',
        body: `
## Authentication

eBay uses OAuth 2.0. Click **Connect** on the eBay credential page to start the OAuth flow. MarketMate redirects you to eBay's authorisation page; on return, the access and refresh tokens are stored encrypted.

## Business Policies

eBay listings require active Business Policies (payment, return, and shipping policies) to be set up in your eBay Seller Hub before you can publish. MarketMate fetches your available policies via the API so you can select them when building a listing.

## Category Suggestions and Aspects

When building a listing, MarketMate can suggest eBay categories based on product keywords, and retrieve the required Item Specifics (aspects) for the chosen category.

## Schema and Auto-Refresh

eBay category schemas (aspects and required fields) are downloaded and cached locally. Auto-refresh can be enabled on a configurable schedule to keep schemas current as eBay updates them.

## eBay Enrichment

MarketMate can enrich your eBay products using the eBay Browse API — fetching additional catalogue data (images, descriptions, specifications) for products that were imported without full detail. Enrichment runs inline during import, and bulk enrichment can be triggered from the Listings view.

## Orders

eBay orders are downloaded via the eBay Fulfillment API. Order sync runs automatically via Cloud Tasks. Tracking numbers pushed via the **Ship** action write back to eBay to mark orders dispatched.

## Catalog Search

The eBay Catalog search endpoint lets you look up eBay catalogue entries by keyword or EAN to find the canonical eBay product record for your item.
`,
      },
      {
        id: 'etsy',
        title: 'Etsy',
        body: `
## Authentication

Etsy uses OAuth 2.0. Click **Connect** on the Etsy credential page and complete the authorisation flow. Tokens are stored encrypted per credential.

## Supported Operations

- **Listings:** Prepare, submit, update, and delete product listings on Etsy.
- **Taxonomy:** Fetch Etsy's category taxonomy and retrieve required properties for a chosen taxonomy node.
- **Shipping Profiles:** Fetch your configured Etsy shipping profiles to assign during listing creation.
- **Image Upload:** Upload images directly to Etsy for use in listings.
- **Orders:** Download Etsy orders and push tracking numbers back on dispatch.

## Listing Flow

1. Use **Prepare** to map your Product data to Etsy's listing fields.
2. Use **Submit** to create the listing on Etsy.
3. Use **Update** to push changes to an existing Etsy listing.

## Orders

Etsy orders are downloaded via the Etsy API. Use the **Import Orders** action on the Etsy credential or the global order import button to trigger a download.
`,
      },
      {
        id: 'bigcommerce',
        title: 'BigCommerce',
        body: `
## Authentication

BigCommerce uses API key authentication. You need: Store Hash, Client ID, and Access Token from your BigCommerce Developer Portal. These are entered directly in the connection form.

## Supported Operations

- **Listings:** Prepare, submit, update, and delete products on your BigCommerce store.
- **Categories:** Fetch the BigCommerce category tree for assigning products to categories during listing creation.
- **Orders:** Download orders from BigCommerce and push tracking on dispatch.
- **Order Status Updates:** Push status changes (e.g. shipped, cancelled) back to BigCommerce.

## Listing Flow

1. Use **Prepare** to map Product data and select a BigCommerce category.
2. Use **Submit** to create the product on BigCommerce.
3. Use **Update** to sync changes to an existing BigCommerce product.

## Webhooks

BigCommerce supports push webhooks for new orders. Register MarketMate's webhook endpoint from the Credential settings page to receive orders in real time rather than polling.
`,
      },
      {
        id: 'back-market',
        title: 'Back Market',
        body: `
## What is Back Market?

Back Market is a marketplace for refurbished electronics. It is active in FR, DE, GB, US, ES, IT, BE, NL, AT, JP, and AU.

## Authentication

Back Market uses API key authentication. Enter your API key in the connection form.

## Supported Operations

- **Listings:** Create and update product listings (offers) on Back Market.
- **Orders:** Download orders and push tracking numbers on dispatch.
- **Inventory Sync:** Push stock level updates to Back Market automatically (inventory sync enabled via credential config).
- **Price Sync:** Push price updates to Back Market.
- **Bulk Order Operations:** Bulk ship and bulk export orders via the Back Market bulk operations endpoints.

## Order Sync

Back Market order sync runs automatically via the platform's order polling scheduler. Manual import is also available from the Back Market credential page.
`,
      },
      {
        id: 'bluepark',
        title: 'Bluepark',
        body: `
## What is Bluepark?

Bluepark is a UK e-commerce platform. MarketMate connects to it for listing management and order import.

## Authentication

Bluepark uses API key authentication. Enter your API key in the connection form.

## Supported Operations

- **Listings:** Prepare and submit product listings to Bluepark.
- **Orders:** Import orders from Bluepark into MarketMate's unified order inbox.

## Listing Flow

1. Use **Prepare** to map Product data to Bluepark's listing format.
2. Use **Submit** to create the listing on Bluepark.

## Orders

Use the **Import Orders** trigger on the Bluepark credential page to download pending orders. The order polling scheduler does not currently auto-poll Bluepark — manual import is recommended.
`,
      },
      {
        id: 'ai-listings',
        title: 'AI Listing Generation',
        body: `
## How AI Generation Works

MarketMate uses a hybrid AI backend (Google Gemini and/or Anthropic Claude, depending on configuration) to generate listing content — titles, descriptions, bullet points, and channel-specific attributes — from your Product data and channel schemas.

## Checking AI Availability

Before running generation, confirm the AI service is available: navigate to the AI generation section and check the status indicator. You can also call the **/ai/status** endpoint directly. The status shows whether Gemini, Claude, or both are configured.

## Generating Content for a Single Product

1. Open a Product or Listing.
2. Click **Generate with AI**.
3. Select the target channel.
4. Review the proposed content in the draft pane.
5. Click **Apply** to write the generated content back to the Listing fields.

Generated content is always a **draft** until you explicitly apply it. Applying is a separate action and does not automatically publish the listing.

## Bulk AI Generation

Bulk generation runs as a Cloud Tasks async job so it does not block the UI.

1. Select multiple products from the Listings or Products list.
2. Click **Bulk Generate**.
3. Monitor progress in **AI Jobs** (visible under the AI menu or via the Sync Status panel).
4. Once complete, review and apply generated content per product.

## Schema-Aware Generation

For Amazon and other schema-driven channels, use **Generate with Schema** to produce content that is pre-mapped to the channel's required fields. This reduces validation errors before submission.

## Configurator AI

The Configurator AI (**/configurators/ai-setup**) assists with setting up listing configurators — the field mapping templates used when pushing products to a channel. It analyses your Product data and suggests appropriate field mappings automatically.
`,
      },
    ],
  },

  // ══════════════════════════════════════════════════════════════════════════
  // OPERATIONS
  // ══════════════════════════════════════════════════════════════════════════
  {
    id: 'operations',
    title: 'Operations',
    icon: '🛒',
    content: [
      {
        id: 'orders',
        title: 'Orders',
        body: `
## Unified Order Inbox

Orders from all connected channels flow into a single inbox at **Operations → Orders**. Each order record stores the channel source, order lines, buyer address, payment status, and fulfilment status.

## Order Statuses

Orders move through statuses such as awaiting_dispatch**, **dispatched**, **cancelled**, and **returned. Statuses can be updated manually or by automation rules.

## Order Actions

From the order detail flyout you can:

- **Hold / Release** — put an order on hold to prevent accidental dispatch.
- **Lock / Unlock** — lock an order to prevent any edits.
- **Tag** — add coloured tags (defined in Settings → Order Tags) for categorisation.
- **Note** — add internal notes visible to your team.
- **Move to Folder** — organise orders into custom folders.
- **Change Shipping Service** — override the carrier/service before dispatching.
- **Process** — move the order through its fulfilment steps.

## Manual Order Creation

Create an order that did not originate on a marketplace via **Orders → New Manual Order**. Useful for phone orders or internal transfers.

## Merge and Split

Select two or more orders and use **Merge** to combine them into a single shipment. Use **Split** on an order to separate its lines into individual shipment records.

## Exporting Orders

Go to **Operations → Import/Export** and select **Export → Orders** to download a CSV of orders with all line and address fields. The export service produces a full-field CSV including order ID, channel, buyer details, lines, and fulfilment data.
`,
      },
      {
        id: 'messages',
        title: 'Messages',
        body: `
## Buyer Messaging

The **Operations → Messages** section provides a helpdesk-style inbox for buyer messages synced from connected marketplaces. Conversations are fetched via the **messaging_handler.go** backend.

## Supported Operations

- View all conversations and unread count.
- Reply to a buyer message.
- Mark conversations as resolved or read.
- Create a new outbound conversation.
- Manage **Canned Responses** — pre-written reply templates for common queries.
- **Sync** — manually pull new messages from all connected channels.

## Unread Badge

The unread message count is displayed as a badge on the Messages nav item, updated each time the page loads or a sync is triggered.

**Assumption to validate:** Channel-level message sync coverage depends on which marketplace APIs support message retrieval. Confirm against individual channel handler implementations if message sync for a specific channel is not working.
`,
      },
      {
        id: 'import-export',
        title: 'Import / Export',
        body: `
## Overview

**Operations → Import/Export** is the general-purpose data hub for bulk-loading and extracting data. It is separate from the marketplace-specific product import.

## Export

Click **Export** and choose the data type:

- **Products** — all product fields.
- **Prices** — SKU and price columns only (for bulk price updates).
- **Stock** — SKU and stock level columns only.
- **Orders** — all order and line fields.
- **RMAs** — return records.
- **Purchase Orders** — PO records.
- **Shipments** — shipment and tracking records.

Exports are delivered as CSV downloads.

## Import Flow

The import process runs in three steps: **Validate → Preview → Apply**.

1. **Validate** — upload your file. MarketMate checks column headers, data types, and required fields. Returns a row-level error report without making any changes.
2. **Preview** — returns the first rows of the parsed file plus an auto-mapping suggestion (matching your column names to system fields).
3. **Apply** — starts a background import job. You can close the browser; the job continues and its status is updated in the Import History.

## File Configuration

When uploading, you can specify:

- **delimiter** — default is comma (**,**).
- **encoding** — default is **utf-8**.
- **has_header_row** — default is **true**.
- **escape_char** — optional character escaping.

## Download Templates

Click **Templates** and select a type to download a pre-formatted CSV with the correct column headers.

## Import History

The last 20 import jobs for your tenant are shown in the history list, each with status, row counts, and a link to the error log if any rows failed.
`,
      },
      {
        id: 'purchase-orders',
        title: 'Purchase Orders',
        body: `
## What is a Purchase Order?

A Purchase Order (PO) is a record of stock ordered from a supplier. MarketMate tracks POs from creation through sending, goods receipt, and closure.

## Creating a Purchase Order

1. Go to **Operations → Purchase Orders → New PO**.
2. Select the supplier.
3. Add line items with SKU, quantity, and cost price.
4. Save the PO. It starts in **draft** status.

## Sending a PO

Click **Send** to mark the PO as sent to the supplier. If the supplier has an email connection configured, a PO email can be triggered automatically via the template system.

## Receiving Goods

When goods arrive, open the PO and click **Receive**. Enter the quantities received per line. Stock levels are updated immediately and the PO is moved to **received** or **partially_received** status.

## Auto-Generate POs

Use **Auto-Generate** to create POs automatically based on reorder suggestions. The system compares current stock levels against reorder points and creates draft POs for items that have fallen below their threshold.

## Reorder Suggestions

Under **Purchase Orders → Reorder Suggestions**, MarketMate shows SKUs that are at or below their reorder point. You can approve a suggestion (converting it to a draft PO), or dismiss it.
`,
      },
      {
        id: 'suppliers',
        title: 'Suppliers',
        body: `
## What is a Supplier?

A Supplier record stores the contact details, account information, and connection settings for a vendor you purchase stock from. Suppliers are linked to Purchase Orders and Reorder Suggestions.

## Creating a Supplier

1. Go to **Operations → Suppliers → New Supplier**.
2. Enter the supplier name, contact email, and any account reference numbers.
3. Optionally configure an EDI or API connection and click **Test Connection** to verify it.
4. Save the Supplier.

## Supplier Returns

When goods need to be returned to a supplier, use the Supplier Return feature. From a Purchase Order, click **Create Return** to log the return, quantities, and reason. Supplier return records are listed under the **Supplier Returns** view.

## Linking Suppliers to Products

On a Product's detail page, you can record the preferred supplier and supplier SKU. This information is used when auto-generating Purchase Orders to pre-fill the supplier field.
`,
      },
    ],
  },

  // ══════════════════════════════════════════════════════════════════════════
  // DISPATCH
  // ══════════════════════════════════════════════════════════════════════════
  {
    id: 'dispatch',
    title: 'Dispatch',
    icon: '🚚',
    content: [
      {
        id: 'dispatch-overview',
        title: 'Dispatch Overview',
        body: `
## End-to-End Dispatch Workflow

1. **Order received** — order arrives in the Operations inbox from any channel.
2. **Fulfilment Source selected** — the warehouse or 3PL that will ship the order is identified. Fulfilment Sources are configured under **Settings → Fulfilment Sources**.
3. **Carrier selected** — choose a carrier and service. Use **Get Rates** to compare prices across configured carriers.
4. **Shipment created** — MarketMate calls the carrier's API to book the shipment and returns a tracking number.
5. **Label generated** — the label PDF is downloaded from the carrier and stored in Google Cloud Storage. A signed download URL is returned to the UI.
6. **Manifest** — at end of day, generate a manifest (close-out) to submit all the day's shipments to the carrier.

## Fulfilment Sources

A Fulfilment Source is any location from which orders are dispatched: a warehouse, a 3PL, or an Amazon FBA centre. Configure Fulfilment Sources at **Settings → Fulfilment Sources**.

Each shipment record stores the Fulfilment Source ID and type (warehouse, 3pl, fba) so you can report on throughput per location.

## Dispatch Console

**Dispatch → Dispatch Console** shows all orders ready for dispatch. Use it to assign carriers, print labels in batch, and track which orders still need action.

## SLA Dashboard

**Dispatch → SLA Dashboard** shows a summary of orders by dispatch deadline, helping you prioritise those at risk of breaching their SLA.

## Delivery Exceptions

**Dispatch → Exceptions** lists shipments that have been flagged by the carrier (failed delivery, address issues, etc.). Acknowledge exceptions to clear them from the queue.
`,
      },
      {
        id: 'carriers',
        title: 'Carriers',
        body: `
## Supported Carriers

| Carrier | Rate Quotes | Labels | Tracking | Manifest |
| --- | --- | --- | --- | --- |
| DPD | ✓ | ✓ | ✓ | ✓ |
| Evri | ✓ | ✓ | ✓ | ✓ |
| Royal Mail | — | ✓ | ✓ | — |
| FedEx | ✓ | ✓ | ✓ | — |

Royal Mail and FedEx do not support electronic manifesting via their APIs. For these carriers, MarketMate generates a **fallback CSV manifest** — a formatted spreadsheet of the day's shipments that can be submitted to the carrier manually.

## Configuring a Carrier

1. Go to **Dispatch → Carriers**.
2. Find the carrier and click **Configure**.
3. Enter the carrier account credentials and click **Test Connection**.
4. Save. The carrier is now available for rate shopping and label creation.

## Rate Shopping

From the order dispatch view, click **Get Rates** to retrieve prices from all configured carriers for the package dimensions and destination. Select the service you want and proceed to create the shipment.

## Carrier Features

Each carrier's capabilities are determined by the **SupportsFeature** method in the carrier adapter. Features include: rate quotes, tracking, signature, international, Saturday delivery, PO Box, customs, insurance, void, manifest, and pickup.
`,
      },
      {
        id: 'pickwaves',
        title: 'Pickwaves',
        body: `
## What is a Pickwave?

A Pickwave is a batch picking job — a grouping of orders that a warehouse operative will pick in a single pass through the warehouse. Pickwaves reduce walking time and improve throughput.

## Creating a Pickwave

1. Go to **Dispatch → Pickwaves → New Pickwave**.
2. Select orders to include (filter by channel, carrier, status, or location).
3. Save the Pickwave. MarketMate generates a consolidated pick list.

## Updating Pickwave Lines

As items are picked, update the status of each line within the Pickwave to record picked quantities. Progress is tracked per line.

## Pickwave Status

Pickwaves move through statuses: draft**, **active**, **completed. Update the status from the Pickwave detail page.

## Generating a Picklist

From the **Orders** view, select a set of orders and use **Generate Picklist** to produce a printable picklist PDF ordered by warehouse bin location.
`,
      },
      {
        id: 'label-printing',
        title: 'Label Printing',
        body: `
## How Labels are Generated

When a shipment is created, MarketMate calls the carrier's API to book the shipment and retrieve the label. Label data is returned as a Base64-encoded PDF and then uploaded to Google Cloud Storage under a path scoped to your tenant, entity type, and shipment ID.

A signed URL (expiring after a configurable number of minutes) is returned to the UI for download or printing.

## Label Print Queue

**Dispatch → Label Printing** shows all shipments with labels ready to print. Select one or more shipments and click **Print** to send them to your label printer (ZPL/PDF depending on carrier format).

## Reprinting a Label

If a label needs to be reprinted, open the Shipment record and use the **Reprint** action to request a fresh signed URL for the stored label file.

## Label Format

DPD returns PDF labels. Evri returns PDF. Royal Mail returns PDF. FedEx returns PDF. Label format is stored on the Shipment record as **LabelFormat**.
`,
      },
      {
        id: 'manifests',
        title: 'End-of-Day Manifests',
        body: `
## What is a Manifest?

A manifest (also called an end-of-day or close-out) is a submission to the carrier confirming all shipments booked that day. Many carriers require a manifest before they will collect parcels.

## Generating a Manifest

1. Go to **Dispatch → Manifests**.
2. Click **Create Manifest**.
3. Select the carrier and date range.
4. MarketMate calls the carrier's manifest API with the list of matching shipments.

## Electronic vs CSV Manifests

**DPD and Evri** support electronic manifesting — the manifest is submitted directly to the carrier's API and a manifest ID is returned.

**Royal Mail and FedEx** do not support electronic manifesting. For these carriers, MarketMate produces a **CSV manifest file** containing all shipment records for the day, which you can download and submit to the carrier's portal manually.

## Manifest History

All manifests (both electronic and CSV) are stored with their manifest ID, carrier, shipment count, and creation timestamp. View them under **Dispatch → Manifests → History**.
`,
      },
      {
        id: 'sla-and-exceptions',
        title: 'SLA and Delivery Exceptions',
        body: `
## SLA Dashboard

**Dispatch → SLA Dashboard** calls the **/dispatch/sla-summary** endpoint to show a breakdown of orders by their dispatch deadline status. Use this to identify orders that are at risk of missing a promised delivery date.

## Delivery Exceptions

**Dispatch → Exceptions** shows shipments flagged by the carrier — for example, failed delivery attempts, return-to-sender events, or address validation failures. The list is returned by the **/dispatch/exceptions** endpoint.

To clear an exception from the queue, open it and click **Acknowledge**. Acknowledged exceptions are removed from the active exception view but remain in the history.

## Address Validation

Before creating a shipment, you can validate the delivery address using **Dispatch → Address Validate**. Bulk address validation is also available for checking multiple orders at once before batch dispatch.

## Dangerous Goods Check

For orders containing items that may be subject to hazmat restrictions, use the **Dangerous Goods Check** action on the order before dispatching to verify carrier acceptability.

## Tracking Writeback

After a shipment is dispatched, use **Writeback Tracking** on the shipment to push the tracking number and carrier back to the originating marketplace order record.
`,
      },
    ],
  },

  // ══════════════════════════════════════════════════════════════════════════
  // INVENTORY
  // ══════════════════════════════════════════════════════════════════════════
  {
    id: 'inventory',
    title: 'Inventory',
    icon: '🏭',
    content: [
      {
        id: 'inventory-overview',
        title: 'Inventory Overview',
        body: `
## What is an Inventory Item?

An Inventory Item tracks the stock of a SKU. Stock is recorded per warehouse location and aggregated to a total. Each location record holds:

- **On-hand** — physical quantity present.
- **Reserved** — quantity allocated to open orders.
- **Available** — on-hand minus reserved (what can be sold).
- **Inbound** — quantity on order from suppliers but not yet received.
- **Safety stock** — minimum buffer quantity below which a reorder alert fires.

## How Totals are Aggregated

When you view a product's stock level, MarketMate sums the **available** quantities across all locations where that SKU is stored. The total is what is reported to channels during inventory sync.

## Reservations

When an order is imported, a **Reservation** is automatically created against the SKUs in that order. This reduces available stock immediately, preventing the same unit from being allocated to a second order (overselling prevention). Reservations are released when the order is dispatched or cancelled.

## Inventory Sync

Stock levels are pushed to all connected channels with inventory sync enabled every 15 minutes via the Inventory Sync Scheduler. Manual sync can also be triggered from the Inventory Sync settings page. Min, max, and buffer rules can be configured per credential to control how much stock is reported to each channel.

## My Inventory View

**Inventory → My Inventory** provides a flat view of all SKUs and their current stock levels. Saved inventory views (column sets and filters) can be created and named for quick access.
`,
      },
      {
        id: 'warehouse-locations',
        title: 'Warehouse Locations',
        body: `
## Multi-Location Tracking

MarketMate supports stock tracking across multiple warehouse locations. A Location can represent a physical site, a zone within a warehouse, or a specific bin/rack position.

## Location Hierarchy

Locations are arranged in a tree: **Site → Zone → Bin/Rack**. Create locations from **Inventory → Locations**.

## Bin and Rack Management

Within a location, you can define Binracks — specific storage positions (shelves, bins, pallet slots). Binracks are managed via **Inventory → Locations → [Location] → Binracks**.

To move stock between binracks, use the **Move Stock** action, which records a stock move event in the audit trail.

## Warehouse Zones

Zones group binracks within a location. Create zones at **Settings → WMS Settings** or directly on the Locations page. Zones support allocation rules — for example, "pick always from Zone A before Zone B".

## Bin Types

Bin types classify the physical container type (pallet, shelf, drawer, etc.). Configure bin types at **Settings → Bin Types**.

## Allocation Rules

Allocation rules define which location (or zone) stock should be picked from first when fulfilling orders. Configure rules at **Inventory → Warehouse Allocation Rules**.

## Replenishment

**Inventory → Picking Replenishment** shows binracks in the pick face whose stock has fallen below a threshold and need to be topped up from bulk storage. This is a warehouse-internal transfer, distinct from supplier replenishment.
`,
      },
      {
        id: 'stock-operations',
        title: 'Stock Operations',
        body: `
## Stock Count

A **Stock Count** is a physical count exercise where you verify actual quantities against the system record.

1. Go to **Inventory → Stock Count → New Count**.
2. Select the location and items to count.
3. Record the counted quantities per line.
4. **Commit** the count to apply any variances as stock adjustments.
5. Or **Cancel** to discard the count without applying changes.

## Stock In

**Inventory → Stock In** (also accessible as a warehouse transfer or via Purchase Order receipt) adds stock to a location. This is the mechanism for recording a goods receipt outside of the formal PO flow.

## Stock Scrap

**Inventory → Stock Scrap** records the removal of damaged or unsellable stock. Enter the SKU, quantity, and reason for scrapping. Scrap events are logged and can be reported on via **Analytics → Reporting**.

## Warehouse Transfers

**Inventory → Warehouse Transfers** records the movement of stock from one location to another. Both the source and destination locations are updated atomically.

## Stock Adjustments

Any stock change (count, scrap, adjustment) is recorded in the adjustment log, which is viewable per product via the **Stock History** tab on the product detail page.
`,
      },
      {
        id: 'forecasting',
        title: 'Demand Forecasting',
        body: `
## What is Forecasting?

The Forecasting module calculates how much stock of each SKU you need to order and when, based on historical sales velocity, lead time, and safety stock buffers.

## Forecasting Settings

Go to **Inventory → Forecasting → Settings** to configure global defaults:

- **Lookback Days** — how many days of sales history to use (default: 90).
- **Lead Time Days** — typical supplier lead time (default: 14).
- **Safety Days** — additional buffer days of stock to hold (default: 7).
- **Auto-Recalculate** — whether forecasts are recalculated automatically when new orders are received.

## Per-Product Overrides

Each SKU can have its own forecasting configuration overriding the global defaults. Open a product, go to the **Forecasting** tab, and set:

- Custom lookback, lead time, or safety days.
- Manual Average Daily Consumption (ADC) if calculated ADC is not suitable.
- Seasonality coefficients (one per month) to adjust for seasonal demand.
- **Just-in-Time mode** — minimises stock holdings by targeting zero buffer.

## Forecast Status Values

| Status | Meaning |
| --- | --- |
| **healthy** | Stock covers lead time + safety days |
| **low** | Stock covers lead time but not safety buffer |
| **critical** | Stock will be exhausted before lead time elapses |
| **overstock** | Stock significantly exceeds forecasted demand |

## Recalculate

Click **Recalculate All** on the Forecasting dashboard to force a fresh calculation across all SKUs. This is useful after a bulk stock import or at the start of a new trading period.
`,
      },
      {
        id: 'replenishment',
        title: 'Replenishment',
        body: `
## Reorder Points

Each SKU has a **Reorder Point** — the stock level at which a reorder should be triggered. The reorder point is calculated as: (ADC × lead time days) + (ADC × safety days). It can also be set manually on the product's forecast config.

## Reorder Quantity

The **Reorder Quantity** is the default amount to order when a reorder is triggered. It is calculated to bring stock back to a level that covers the lookback period's demand, adjusted for safety stock.

## Auto-Reorder

The Auto-Reorder service runs periodically and checks all SKUs across all tenants. When a SKU's available stock is at or below its reorder point, the service creates a **Reorder Suggestion**.

Reorder Suggestions are reviewed at **Operations → Purchase Orders → Reorder Suggestions**:

- **Approve** a suggestion to convert it into a draft Purchase Order.
- **Dismiss** to ignore the suggestion (a new one will be generated on the next run if stock remains low).

## Manual Reorder Check

You can trigger an immediate reorder check from the Forecasting dashboard via **Forecasting → Auto-Reorder → Run Check**.

## Low-Stock Notifications

The Stock Alert Service runs every 6 hours and fires in-app notifications (with a 24-hour cooldown per SKU) when items are at or below their reorder point. Notifications appear in the notification bell in the top bar.
`,
      },
      {
        id: 'fba-inbound',
        title: 'Amazon FBA Inbound',
        body: `
## What is FBA Inbound?

Amazon FBA (Fulfilled by Amazon) inbound shipments are the process of sending your stock to Amazon's fulfilment centres so that Amazon can store and ship orders on your behalf.

## Creating an FBA Inbound Shipment

1. Go to **Inventory → FBA Inbound → New Shipment**.
2. Select the Amazon credential.
3. Add the SKUs and quantities to send.
4. Click **Plan** to submit the inbound shipment plan to Amazon's API. Amazon returns the fulfilment centre assignment and any splitting of items across centres.
5. Click **Confirm** to confirm the plan and generate the FBA shipment ID.
6. Pack and ship the stock to the assigned fulfilment centre.
7. Click **Close** when the shipment has been physically sent.

## Tracking FBA Inbound Stock

Inbound quantities are reflected in the **Inbound** field of the Inventory Item record for the SKU. Once Amazon receives and processes the stock, the inbound quantity is released to FBA available stock.

## FBA as a Fulfilment Source

FBA fulfilment centres are configured as Fulfilment Sources of type **fba**. Orders dispatched via FBA are recorded with the appropriate Fulfilment Source.
`,
      },
    ],
  },

  // ══════════════════════════════════════════════════════════════════════════
  // RETURNS
  // ══════════════════════════════════════════════════════════════════════════
  {
    id: 'returns',
    title: 'Returns',
    icon: '↩️',
    content: [
      {
        id: 'returns-overview',
        title: 'Returns Overview',
        body: `
## The Returns Process

MarketMate manages the full returns lifecycle from the moment a buyer initiates a return through to restock or disposal and refund issuance. Returns are tracked as **RMA** (Return Merchandise Authorisation) records.

## How Returns Arrive

Returns can arrive in two ways:

1. **Marketplace sync** — use **RMAs → Sync** to pull pending return requests from connected marketplaces. This calls the marketplace's returns API and creates RMA records automatically.
2. **Manual creation** — create an RMA manually from **Returns → New RMA**, linking it to an existing order.

## Returns Portal

A public-facing returns portal is available at **/api/v1/public/returns/** so that buyers can self-serve their return requests without logging into your system. Configure the portal appearance and rules via **RMAs → Config**.

## Refund Downloads

Refund events from Amazon, eBay, and Shopify can be downloaded and matched to RMA records via the **Refund Downloads** section. This keeps your RMA records in sync with marketplace-initiated refunds.
`,
      },
      {
        id: 'rmas',
        title: 'RMAs',
        body: `
## RMA Lifecycle

An RMA moves through the following statuses:

1. **requested** — buyer has requested a return.
2. **authorised** — you have approved the return and issued the buyer a return reference.
3. **received** — the returned goods have arrived at your warehouse.
4. **inspected** — each returned line has been assessed for condition.
5. **restocked** / **scrapped** — items in acceptable condition are restocked; damaged items are written off.
6. **resolved** — the RMA is closed with a final resolution (refund issued, exchange sent, etc.).

## Processing an RMA

1. Go to **Returns → RMAs** and open the RMA.
2. Click **Authorise** to approve the return and advance the status.
3. When goods arrive, click **Receive** and enter the quantities received per line.
4. Click **Inspect** to record the condition (new, used, damaged) and disposition (restock, scrap, return to supplier) per line.
5. For items to restock, click **Restock Line** to add the quantity back to inventory.
6. Click **Resolve** to close the RMA, recording the final outcome.

## Actionable RMA Count

The **Returns** nav item shows a badge with the count of RMAs that are in an actionable state (not yet resolved). This is updated each time the RMA list is loaded.

## Pushing a Refund

From a resolved RMA, click **Push Refund** to submit the refund amount back to the originating marketplace via the marketplace's refund API. This is available for Amazon, eBay, and Shopify.

## RMA Number Format

RMA numbers are auto-generated in the format RMA-{year}-{sequence}**, for example **RMA-2026-0042.
`,
      },
    ],
  },

  // ══════════════════════════════════════════════════════════════════════════
  // AUTOMATION
  // ══════════════════════════════════════════════════════════════════════════
  {
    id: 'automation',
    title: 'Automation',
    icon: '⚙️',
    content: [
      {
        id: 'automation-overview',
        title: 'Automation Overview',
        body: `
## What is Automation?

MarketMate provides two complementary automation systems:

**Automation Rules** are lightweight condition/action rules evaluated by the Rule Engine. They can react to events (such as a new order arriving) or run on a schedule.

**Workflows** are multi-step processing pipelines that an order passes through. A Workflow defines a sequence of steps and can branch, loop, and call external actions. Both systems are available under the **Automation** sidebar section.

## Event-Driven Rules

Event-driven rules fire when a specific event occurs within the platform — for example, order.created**, **order.status_changed**, or **inventory.low_stock. When the event fires, the Rule Engine evaluates all active rules with that trigger and executes any whose conditions match.

## Scheduled Rules

Scheduled rules run on a cron schedule (e.g. daily at 08:00). The CronScheduler loads scheduled rules for all tenants on server startup and registers them with the underlying cron library. Use scheduled rules for tasks like daily low-stock summaries, nightly price adjustments, or weekly reporting emails.

## MacroScheduler

The MacroScheduler runs every minute and processes any macros (bulk rule actions) that are due. It supports daily, weekly, and monthly schedules, and can trigger actions such as sending emails via the configured SMTP server.

## Script Pre-Validation

Any rule that includes a script has its script syntax checked before the rule is saved. Rules with validation errors are rejected.
`,
      },
      {
        id: 'creating-rules',
        title: 'Creating Automation Rules',
        body: `
## Creating a Rule

1. Go to **Automation → Automation Rules → New Rule**.
2. Enter a name for the rule.
3. Select a **Trigger** — the event or schedule that fires the rule.
4. Add **Conditions** — filter criteria that must be true for the rule actions to execute. Conditions use the field operators available via the **/automation/fields** endpoint.
5. Define **Actions** — what the rule does when triggered. Available actions are listed via the **/automation/actions** endpoint.
6. Optionally set a **Schedule** (cron expression) if this is a scheduled rule rather than event-driven.
7. Click **Save**. The rule is saved in **inactive** state by default.
8. Toggle the rule **Active** to enable it.

## Available Trigger Types

Trigger types are filterable via the **trigger** query parameter on the rule list. Common triggers include order lifecycle events, inventory threshold events, and scheduled time-based triggers. The full list is returned by the **/automation/fields** endpoint.

## Testing a Rule

Before activating a new rule, use **Test Rule** to simulate the rule against a sample order or event payload. The test returns the evaluation result and which actions would have fired.

## Duplicating a Rule

Click **Duplicate** on any rule to create a copy. Duplicates start inactive so you can modify them safely before enabling.
`,
      },
      {
        id: 'workflows',
        title: 'Workflows',
        body: `
## What is a Workflow?

A Workflow is a multi-step automated pipeline applied to an order. It is more powerful than an Automation Rule when you need sequential logic, branching decisions, or multiple ordered actions.

## Creating a Workflow

1. Go to **Automation → Workflows → New Workflow**.
2. Name the workflow.
3. Add steps. Each step can be a condition check, an action, a delay, or a sub-flow branch.
4. Set the activation trigger (typically an order event).
5. Save and click **Activate**.

## Workflow Simulator

Use **Automation → Workflow Simulator** to test a workflow against a simulated order payload before activating it in production. The simulator returns the step-by-step execution trace and final outcome.

## Workflow Execution History

Open a Workflow and go to the **Executions** tab to see a log of every time the workflow has run, its outcome, and any errors encountered per step.

## Processing an Order Through Workflows

Workflows run automatically when a matching order event fires. You can also manually trigger workflow processing on a specific order from the order's Actions menu using **Process Workflows**.
`,
      },
      {
        id: 'automation-logs',
        title: 'Automation Logs',
        body: `
## What are Automation Logs?

Automation Logs record every execution of an Automation Rule — whether it succeeded, was skipped (conditions not met), or failed. Logs are stored per tenant and are accessible at **Automation → Automation Logs**.

## Reading a Log Entry

Each log entry shows:

- **Rule name** — which rule fired.
- **Trigger** — the event that caused it to fire.
- **Status** — success**, **skipped**, or **failed.
- **Timestamp** — when the execution occurred.
- **Details** — the evaluated conditions, actions attempted, and any error message if the execution failed.

## Diagnosing a Failed Rule

1. Find the failed log entry and open its details.
2. Check the **Conditions** section — if conditions were not met, the rule was skipped, not failed.
3. Check the **Actions** section — the error message identifies which action failed and why (e.g. email SMTP error, invalid field reference, API call failure).
4. Correct the rule configuration and re-save.

## Retrying a Log Entry

From the log detail view, click **Retry** to re-execute the rule for the same payload. Useful after fixing a configuration error.

## Clearing Logs

Click **Clear Logs** to delete historical log entries for your tenant. This is a bulk delete and cannot be undone.
`,
      },
    ],
  },

  // ══════════════════════════════════════════════════════════════════════════
  // ANALYTICS
  // ══════════════════════════════════════════════════════════════════════════
  {
    id: 'analytics',
    title: 'Analytics',
    icon: '📈',
    content: [
      {
        id: 'analytics-overview',
        title: 'Analytics Overview',
        body: `
## Overview Dashboard

**Analytics → Overview** provides a summary of your business performance. The following metrics are available (confirmed in **analytics_handler.go**):

- **Revenue** — total order revenue over a selected period, broken down by channel.
- **Orders** — order counts by status and by channel.
- **Top Products** — best-selling SKUs by revenue or units.
- **Inventory Health** — percentage of SKUs in healthy, low, critical, and overstock states.
- **Returns** — return rate and RMA counts.
- **Home Dashboard** — a configurable home page widget set.
- **Stock Consumption** — rate at which stock is being consumed per SKU.

## Channel P&L

**Analytics → Channel P&L** breaks down revenue, returns, and costs per marketplace channel, enabling you to compare profitability across channels.

## Listing Health

**Analytics → Listing Health** shows a bulk health score per listing — identifying listings with missing images, poor descriptions, or low sell-through rates.

## Reconciliation Health

**Analytics → Reconciliation Health** shows the status of SKU-to-listing reconciliation across channels, flagging unmatched listings.
`,
      },
      {
        id: 'dashboards',
        title: 'Dashboards',
        body: `
## Inventory Dashboard

**Analytics → Inventory Dashboard** provides a stock-level view across all SKUs and locations, including inbound stock, low-stock counts, and overstock flags. Available via the **/analytics/inventory-dashboard** endpoint.

## Order Dashboard

**Analytics → Order Dashboard** shows order processing performance — orders received, dispatched, and outstanding by day, channel, and carrier. Available via **/analytics/order-dashboard**.

## Pivot Analytics

**Analytics → Pivot Analytics** is a flexible pivot table builder. Select fields from the available field list (**/analytics/pivot/fields**) and run an ad-hoc analysis. Results can be exported as CSV.

## Operational Dashboard

**Analytics → Operational Dashboard** shows real-time operational health including channel sync status, warehouse throughput, and dispatch SLA performance. Available via **/analytics/operational**.

## Pre-Built Reports

The following pre-built reports are available under **Analytics → Reporting**:

- Orders by Channel
- Orders by Date
- Orders by Product
- Despatch Performance
- Returns Report
- Financial Report

Each report returns filtered data and can be exported as CSV via the **/analytics/reports/export** endpoint.
`,
      },
      {
        id: 'report-builder',
        title: 'Report Builder',
        body: `
## What is the Report Builder?

The Report Builder at **Analytics → Reporting** allows you to create custom reports by selecting an entity type, choosing fields, applying filters, and running the report. Reports can be saved and re-run.

## Creating a Report

1. Go to **Analytics → Reporting → New Report**.
2. Select the entity (Orders, Products, Inventory, Shipments, RMAs, or Purchase Orders).
3. Use **Get Fields** to see the available columns for the selected entity.
4. Select the columns to include.
5. Apply date range and status filters.
6. Click **Run** to execute the report.
7. Click **Save** to name and store the report for future use.

## Saved Reports

Previously saved reports are listed at **Analytics → Reporting → Saved Reports**. Click any saved report to re-run it with the current data.

## Export

All report results can be exported as CSV from the report results view.
`,
      },
    ],
  },

  // ══════════════════════════════════════════════════════════════════════════
  // TROUBLESHOOTING
  // ══════════════════════════════════════════════════════════════════════════
  {
    id: 'troubleshooting',
    title: 'Troubleshooting',
    icon: '🔧',
    content: [
      {
        id: 'common-issues',
        title: 'Common Issues',
        body: `
## Missing tenant_id error

**Symptom:** API returns **tenant ID required** or **400 Bad Request** across any operation.

**Check:** Confirm that the **X-Tenant-Id** header is being sent with every API request. This header is injected automatically by the frontend using the active tenant context.

**Fix:** Log out and back in. If using the API directly, ensure **X-Tenant-Id** is set correctly in your request headers.

---

## Import job goes straight to failed

**Symptom:** An import job shows **failed** status immediately after starting.

**Check:** Open the import job detail and read the error log. Common causes are: malformed CSV (encoding issue, wrong delimiter), missing required columns, or a marketplace API credential error.

**Fix:** Download the template for the relevant import type, compare your file's column headers against the template, correct the file, and re-import.

---

## Marketplace credential errors

**Symptom:** A channel connection test fails or listings refuse to publish with an authentication error.

**Check:** Open the Credential in **Marketplace → Connections** and click **Test Connection**. The test response will include the specific error from the channel API.

**Fix:** For OAuth channels (Amazon, eBay, Etsy), reconnect the account via the OAuth flow — the stored refresh token may have expired. For API key channels, verify the key has not been revoked in the channel's developer portal.

---

## eBay OAuth callback not completing

**Symptom:** After approving on eBay, the redirect does not return to MarketMate, or the credential is not saved.

**Check:** Confirm that the **BACKEND_URL** environment variable is set to the public URL of the backend server. The eBay RuName registered in the eBay Developer Console must match the redirect URI exactly.

**Fix:** Update the redirect URI in the eBay Developer Console to match **{BACKEND_URL}/api/v1/ebay/oauth/callback**.

---

## Amazon SP-API LWA vs IAM confusion

**Symptom:** Amazon returns **401 Unauthorized** or **403 Forbidden** on listing or order calls despite a valid OAuth token.

**Check:** Amazon requires both LWA credentials (for the access token) and AWS IAM credentials (for request signing). Confirm both sets are present in the credential record.

**Fix:** Ensure all eight Amazon credential fields are populated: lwa_client_id**, **lwa_client_secret**, **refresh_token**, **aws_access_key_id**, **aws_secret_access_key**, **region**, **sp_endpoint**, and **marketplace_id.

---

## AI generation job stuck

**Symptom:** A bulk AI generation job shows **processing** but does not progress.

**Check:** Go to **/ai/status** to confirm the AI service is available. Check the Sync Status panel for the job's current progress and any error message.

**Fix:** If the AI service is unavailable, verify that **GEMINI_API_KEY** and/or **CLAUDE_API_KEY** environment variables are set on the backend. If the job is stuck in Cloud Tasks, it can be retried from the Ops console.

---

## Carrier label not generating

**Symptom:** Shipment creation returns an error or produces no label PDF.

**Check:** Open **Dispatch → Carriers → [Carrier] → Credentials** and run **Test Connection**. Check the error message — common causes are invalid account credentials, an incomplete delivery address, or a package weight/dimension outside the carrier's accepted range.

**Fix:** Correct the address fields on the order (all carrier APIs require house number, postcode, and country at minimum). Update carrier credentials if the account has changed.

---

## Stale stock reservations making available quantity look wrong

**Symptom:** Available stock is lower than expected; on-hand quantity is correct.

**Check:** Go to the product's Inventory tab and view the reservations list. Check for open reservations linked to orders that have been cancelled or dispatched without the reservation being released.

**Fix:** Cancelled orders automatically release their reservation. If a reservation is orphaned, use the stock reservation management endpoints to manually release it. Confirmed in **stock_reservation_handler.go**.
`,
      },
      {
        id: 'error-reference',
        title: 'Error Reference',
        body: `
## Common Error Patterns

| Symptom | Likely Cause | Where to Check | Fix |
| --- | --- | --- | --- |
| **tenant ID required** | Missing X-Tenant-Id header | All handlers | Add X-Tenant-Id to request headers |
| **403 Forbidden** on Amazon catalog API | Missing Catalog Items role in Seller Central | SP-API Developer Console | Enable Catalog Items role on the SP-API application |
| Import job **failed** immediately | File format error or missing columns | Import job error log | Download template and reformat file |
| eBay **invalid_grant** on token refresh | Expired or revoked OAuth token | eBay Developer Console | Reconnect via OAuth flow |
| **circuit breaker open** in import log | 3+ consecutive enrichment API failures | Cloud Run logs | Check SP-API error type on first 3 ASINs |
| Carrier **address validation failed** | Incomplete delivery address | Order address fields | Fill in all required address fields before dispatching |
| Automation rule **script error** | Syntax error in rule script | Automation Rules detail | Fix script syntax; platform validates before save |
| Stock available = 0 unexpectedly | Unreleased reservation from cancelled order | Stock Reservations | Manually release orphaned reservation |
| AI generation returns empty content | AI service not configured | /ai/status endpoint | Set GEMINI_API_KEY or CLAUDE_API_KEY environment variable |
| Listing submission **schema validation failed** | Missing required Amazon product type field | Amazon schema download | Download and review product type schema |
`,
      },
      {
        id: 'escalation',
        title: 'Escalating to Engineering',
        body: `
## When to Escalate

Escalate to the engineering team when:

- An import job is stuck in **processing** for more than 30 minutes with no progress.
- A Cloud Tasks job cannot be retried via the Ops console.
- Firestore data appears inconsistent (e.g. stock totals do not match individual location records).
- A marketplace channel API returns errors that are not resolved by reconnecting credentials.
- The AI service reports as available but generation consistently fails silently.

## Information to Gather Before Escalating

Collect the following before raising an issue:

- **Tenant ID** — visible in the URL or tenant switcher.
- **Job ID** — for import jobs, AI jobs, or sync tasks (visible in the job detail or history list).
- **Timestamp** — when the issue occurred (in UTC if possible).
- **Error message** — the exact error text from the UI, job log, or Ops console.
- **Channel and Credential ID** — for marketplace-related issues.
- **Steps to reproduce** — what action triggered the problem.

## What Cannot be Self-Served

- Firestore document-level corrections.
- Cloud Tasks queue draining or dead-letter queue review.
- PII key rotation (managed via the **gen_pii_keys** CLI tool in **backend/cmd/**).
- Tenant provisioning and plan overrides (admin-only API endpoints).
`,
      },
    ],
  },

  // ══════════════════════════════════════════════════════════════════════════
  // FAQ
  // ══════════════════════════════════════════════════════════════════════════
  {
    id: 'faq',
    title: 'FAQ',
    icon: '❓',
    content: [
      {
        id: 'faq-platform',
        title: 'Platform Questions',
        body: `
## Which marketplaces are supported?

Fully supported channels with listing and order management include Amazon, eBay, Etsy, BigCommerce, Back Market, Bluepark, TikTok Shop, WooCommerce, Walmart, Kaufland, Magento 2, and OnBuy. A generic Mirakl integration covers Tesco, B&Q, Superdrug, Debenhams, Decathlon, Mountain Warehouse, JD Sports, Carrefour, Fnac Darty, Leroy Merlin, MediaMarkt, ASOS, Macy's, and Lowe's. Shopify is registered and active with listing, inventory sync, and price sync capabilities.

---

## How is my data kept separate from other customers?

All data is stored in Google Cloud Firestore under a tenant-scoped path (**tenants/{tenant_id}/**). Each API request is authenticated and the tenant ID is validated by the auth middleware before any data is read or written. One tenant's data is never accessible to another tenant's requests.

---

## How are marketplace API credentials stored?

Credentials are encrypted using AES-256 before being written to Firestore. The encryption key is set by the platform operator via the **CREDENTIAL_ENCRYPTION_KEY** environment variable and is never stored alongside the credentials it protects.

---

## Can I connect multiple accounts for the same marketplace?

Yes. Each account is stored as a separate Credential record. When importing products, starting an order download, or publishing listings, you select which Credential to use. There is no hard limit on the number of credentials per channel.

---

## Is there an API?

Yes. MarketMate exposes a REST API at **/api/v1/**. All endpoints require a valid Firebase Authentication token (passed as a Bearer token in the **Authorization** header) and an **X-Tenant-Id** header. The full route list is defined in **backend/main.go** in the **setupRouter** function.
`,
      },
      {
        id: 'faq-products',
        title: 'Product and Import Questions',
        body: `
## How do I add products in bulk?

Use the Import flow at **Operations → Import/Export → Import**. Download the Products template, populate it with your product data, and upload it. The import runs through a validate → preview → apply sequence so you can check for errors before committing.

---

## What file format does import expect?

CSV is the primary format. Default delimiter is comma, default encoding is UTF-8, and header row is expected. These can be overridden in the upload form. XLSX import is available for specific workflows (such as the Temu Wizard).

---

## Can I undo an import?

There is no automatic rollback for a completed import. If an import created incorrect records, you will need to correct them manually or via a follow-up bulk import. For this reason, always use the **Validate** and **Preview** steps before applying an import.

---

## How long does an import take?

Small imports (fewer than 500 rows) typically complete in under 60 seconds. Large imports run as background jobs via Cloud Tasks and may take several minutes. Progress is shown in real time in the Import Jobs view.

---

## What happens to variants during import?

If your import file contains rows with the same parent product identifier but different variant attributes, MarketMate creates or updates the parent Product and creates each Variant as a child record with its own SKU. Variant rows must include the SKU column to be matched correctly.
`,
      },
      {
        id: 'faq-operations',
        title: 'Orders, Inventory, and Dispatch Questions',
        body: `
## What happens if an order comes in for an out-of-stock item?

The order is still imported and a Reservation is attempted. If the available stock is zero, the Reservation will reduce available to a negative value (depending on configuration). A low-stock notification is fired. The order can be processed once stock is replenished. Consider configuring inventory sync buffer rules to prevent overselling.

---

## How do I add a new warehouse location?

Go to **Inventory → Locations → New Location**. Enter a name, select a parent location if applicable (to place it within an existing site or zone), and save. Stock can then be allocated to the new location via Stock In or transfer.

---

## Which carriers are supported?

DPD, Evri, Royal Mail, and FedEx are the supported carrier integrations. Each requires its own account credentials configured under **Dispatch → Carriers**.

---

## How do I generate an end-of-day manifest?

Go to **Dispatch → Manifests → Create Manifest**, select the carrier and date, and submit. DPD and Evri support electronic manifesting. Royal Mail and FedEx produce a CSV file for manual submission.

---

## How do automation rules interact with orders?

When an order is imported or its status changes, the Rule Engine evaluates all active automation rules with matching event triggers. Rules whose conditions match the order's fields execute their defined actions (e.g. assign a tag, send an email, update a field, trigger a workflow). All rule executions are logged in **Automation → Automation Logs**.
`,
      },
      {
        id: 'faq-ai',
        title: 'AI Questions',
        body: `
## How does AI listing generation work?

AI generation analyses your Product's title, description, attributes, and the target channel's schema to produce channel-optimised listing content. For schema-driven channels such as Amazon, the output is pre-mapped to the product type's required fields. The AI backend supports Google Gemini, Anthropic Claude, or both in hybrid mode, depending on which API keys are configured.

---

## Is AI-generated content applied automatically?

No. Generated content is always saved as a draft and shown for review before you apply it. You must explicitly click **Apply** to write the content into the Listing fields. This ensures you remain in control of what is published.

---

## What do I do if AI generation fails?

Check **/ai/status** to confirm the AI service is available. If the service shows as unavailable, verify that **GEMINI_API_KEY** and/or **CLAUDE_API_KEY** environment variables are correctly set on the backend. If the service is available but generation returns an error, check the AI Jobs log for the specific error message. Failed bulk generation jobs can be retried from the Ops console.
`,
      },
    ],
  },

];


// ─── Markdown-lite renderer ───────────────────────────────────────────────────

function renderBody(text: string): JSX.Element[] {
  const lines = text.split('\n');
  const elements: JSX.Element[] = [];
  let key = 0;
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];

    if (line.startsWith('## ')) {
      elements.push(
        <h2 key={key++} className="help-h2">{line.slice(3)}</h2>
      );
    } else if (line.startsWith('### ')) {
      elements.push(
        <h3 key={key++} className="help-h3">{line.slice(4)}</h3>
      );
    } else if (line.startsWith('---')) {
      elements.push(<hr key={key++} className="help-hr" />);
    } else if (line.startsWith('| ')) {
      // Table block
      const tableLines: string[] = [];
      while (i < lines.length && lines[i].startsWith('|')) {
        tableLines.push(lines[i]);
        i++;
      }
      const headers = tableLines[0].split('|').filter(c => c.trim()).map(c => c.trim());
      const rows = tableLines.slice(2).map(r => r.split('|').filter(c => c.trim()).map(c => c.trim()));
      elements.push(
        <div key={key++} className="help-table-wrap">
          <table className="help-table">
            <thead>
              <tr>{headers.map((h, hi) => <th key={hi}>{inlineFormat(h)}</th>)}</tr>
            </thead>
            <tbody>
              {rows.map((row, ri) => (
                <tr key={ri}>{row.map((cell, ci) => <td key={ci}>{inlineFormat(cell)}</td>)}</tr>
              ))}
            </tbody>
          </table>
        </div>
      );
      continue;
    } else if (line.match(/^\d+\. /) || line.startsWith('- ') || line.startsWith('* ')) {
      // List block
      const listLines: string[] = [];
      const isOrdered = line.match(/^\d+\. /);
      while (i < lines.length && (lines[i].match(/^\d+\. /) || lines[i].startsWith('- ') || lines[i].startsWith('* '))) {
        listLines.push(lines[i]);
        i++;
      }
      const Tag = isOrdered ? 'ol' : 'ul';
      elements.push(
        <Tag key={key++} className="help-list">
          {listLines.map((l, li) => {
            const content = l.replace(/^(\d+\. |- |\* )/, '');
            return <li key={li}>{inlineFormat(content)}</li>;
          })}
        </Tag>
      );
      continue;
    } else if (line.trim() !== '') {
      elements.push(
        <p key={key++} className="help-p">{inlineFormat(line)}</p>
      );
    }

    i++;
  }

  return elements;
}

function inlineFormat(text: string): (string | JSX.Element)[] {
  const parts: (string | JSX.Element)[] = [];
  let remaining = text;
  let key = 0;

  while (remaining.length > 0) {
    const boldIdx = remaining.indexOf('**');
    const codeIdx = remaining.indexOf('`');

    const nextIdx = Math.min(
      boldIdx === -1 ? Infinity : boldIdx,
      codeIdx === -1 ? Infinity : codeIdx
    );

    if (nextIdx === Infinity) {
      parts.push(remaining);
      break;
    }

    if (nextIdx > 0) {
      parts.push(remaining.slice(0, nextIdx));
      remaining = remaining.slice(nextIdx);
    }

    if (remaining.startsWith('**')) {
      const end = remaining.indexOf('**', 2);
      if (end === -1) { parts.push(remaining); break; }
      parts.push(<strong key={key++}>{remaining.slice(2, end)}</strong>);
      remaining = remaining.slice(end + 2);
    } else if (remaining.startsWith('`')) {
      const end = remaining.indexOf('`', 1);
      if (end === -1) { parts.push(remaining); break; }
      parts.push(<code key={key++} className="help-code">{remaining.slice(1, end)}</code>);
      remaining = remaining.slice(end + 1);
    }
  }

  return parts;
}

// ─── Component ────────────────────────────────────────────────────────────────

interface HelpDrawerProps {
  isOpen: boolean;
  onClose: () => void;
}

export default function HelpDrawer({ isOpen, onClose }: HelpDrawerProps) {
  const [activeSection, setActiveSection] = useState<string>(DOCS[0].id);
  const [activePage, setActivePage] = useState<string>(DOCS[0].content[0].id);
  const [search, setSearch] = useState('');
  const [searchResults, setSearchResults] = useState<{ sectionTitle: string; page: DocPage }[]>([]);

  // Close on Escape
  useEffect(() => {
    const handler = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [onClose]);

  // Search
  useEffect(() => {
    if (!search.trim()) { setSearchResults([]); return; }
    const q = search.toLowerCase();
    const results: { sectionTitle: string; page: DocPage }[] = [];
    for (const section of DOCS) {
      for (const page of section.content) {
        if (page.title.toLowerCase().includes(q) || page.body.toLowerCase().includes(q)) {
          results.push({ sectionTitle: section.title, page });
        }
      }
    }
    setSearchResults(results);
  }, [search]);

  const currentPage = (() => {
    for (const section of DOCS) {
      for (const page of section.content) {
        if (page.id === activePage) return page;
      }
    }
    return DOCS[0].content[0];
  })();

  const selectPage = (sectionId: string, pageId: string) => {
    setActiveSection(sectionId);
    setActivePage(pageId);
    setSearch('');
  };

  if (!isOpen) return null;

  return (
    <>
      {/* Backdrop */}
      <div className="help-backdrop" onClick={onClose} />

      {/* Drawer */}
      <aside className="help-drawer">
        {/* Header */}
        <div className="help-header">
          <div className="help-header-title">
            <span className="help-header-icon">?</span>
            <span>Help Centre</span>
          </div>
          <button className="help-close" onClick={onClose} aria-label="Close help">✕</button>
        </div>

        {/* Search */}
        <div className="help-search-wrap">
          <input
            className="help-search"
            type="text"
            placeholder="Search documentation…"
            value={search}
            onChange={e => setSearch(e.target.value)}
          />
        </div>

        {/* Body */}
        <div className="help-body">
          {/* Sidebar nav */}
          <nav className="help-nav">
            {DOCS.map(section => (
              <div key={section.id} className="help-nav-section">
                <button
                  className={`help-nav-section-title ${activeSection === section.id ? 'active' : ''}`}
                  onClick={() => {
                    setActiveSection(section.id);
                    setActivePage(section.content[0].id);
                    setSearch('');
                  }}
                >
                  <span className="help-nav-icon">{section.icon}</span>
                  {section.title}
                </button>
                {activeSection === section.id && (
                  <ul className="help-nav-pages">
                    {section.content.map(page => (
                      <li key={page.id}>
                        <button
                          className={`help-nav-page ${activePage === page.id ? 'active' : ''}`}
                          onClick={() => selectPage(section.id, page.id)}
                        >
                          {page.title}
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            ))}
          </nav>

          {/* Content */}
          <main className="help-content">
            {search.trim() ? (
              <div>
                <h2 className="help-h2">Search results for "{search}"</h2>
                {searchResults.length === 0 ? (
                  <p className="help-p">No results found. Try different keywords.</p>
                ) : (
                  searchResults.map(({ sectionTitle, page }) => (
                    <div key={page.id} className="help-search-result" onClick={() => {
                      const section = DOCS.find(s => s.content.some(p => p.id === page.id));
                      if (section) selectPage(section.id, page.id);
                    }}>
                      <div className="help-search-result-meta">{sectionTitle}</div>
                      <div className="help-search-result-title">{page.title}</div>
                      <div className="help-search-result-excerpt">
                        {page.body.replace(/[#*`|]/g, '').slice(0, 120)}…
                      </div>
                    </div>
                  ))
                )}
              </div>
            ) : (
              <article>
                <h1 className="help-h1">{currentPage.title}</h1>
                {renderBody(currentPage.body)}
              </article>
            )}
          </main>
        </div>
      </aside>

      <style>{`
        .help-backdrop {
          position: fixed; inset: 0;
          background: rgba(0,0,0,0.4);
          z-index: 900;
          animation: helpFadeIn 0.2s ease;
        }
        .help-drawer {
          position: fixed; top: 0; right: 0; bottom: 0;
          width: min(820px, 95vw);
          background: #fff;
          display: flex; flex-direction: column;
          z-index: 901;
          box-shadow: -4px 0 32px rgba(0,0,0,0.18);
          animation: helpSlideIn 0.25s cubic-bezier(0.16,1,0.3,1);
          font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
        }
        @keyframes helpFadeIn { from { opacity:0 } to { opacity:1 } }
        @keyframes helpSlideIn { from { transform: translateX(100%) } to { transform: translateX(0) } }

        .help-header {
          display: flex; align-items: center; justify-content: space-between;
          padding: 16px 20px;
          border-bottom: 1px solid #e5e7eb;
          background: #1e293b;
          color: #fff;
          flex-shrink: 0;
        }
        .help-header-title {
          display: flex; align-items: center; gap: 10px;
          font-size: 15px; font-weight: 600; letter-spacing: 0.01em;
        }
        .help-header-icon {
          width: 26px; height: 26px;
          background: #3b82f6; border-radius: 50%;
          display: flex; align-items: center; justify-content: center;
          font-size: 13px; font-weight: 700; color: #fff;
          flex-shrink: 0;
        }
        .help-close {
          background: none; border: none; color: #94a3b8;
          font-size: 18px; cursor: pointer; padding: 4px 8px;
          border-radius: 4px; transition: color 0.15s;
        }
        .help-close:hover { color: #fff; }

        .help-search-wrap {
          padding: 12px 20px;
          border-bottom: 1px solid #e5e7eb;
          flex-shrink: 0;
          background: #f8fafc;
        }
        .help-search {
          width: 100%; box-sizing: border-box;
          padding: 8px 12px; border: 1px solid #d1d5db;
          border-radius: 6px; font-size: 13px;
          outline: none; background: #fff;
          transition: border-color 0.15s;
        }
        .help-search:focus { border-color: #3b82f6; box-shadow: 0 0 0 2px rgba(59,130,246,0.15); }

        .help-body {
          display: flex; flex: 1; overflow: hidden;
        }

        .help-nav {
          width: 200px; flex-shrink: 0;
          overflow-y: auto;
          border-right: 1px solid #e5e7eb;
          padding: 12px 0;
          background: #f8fafc;
        }
        .help-nav-section { margin-bottom: 2px; }
        .help-nav-section-title {
          display: flex; align-items: center; gap: 8px;
          width: 100%; padding: 7px 16px;
          background: none; border: none; cursor: pointer;
          font-size: 12px; font-weight: 600; color: #374151;
          text-align: left; transition: background 0.1s;
          text-transform: uppercase; letter-spacing: 0.04em;
        }
        .help-nav-section-title:hover { background: #e2e8f0; }
        .help-nav-section-title.active { color: #1d4ed8; }
        .help-nav-icon { font-size: 14px; }

        .help-nav-pages { list-style: none; margin: 0; padding: 0 0 4px; }
        .help-nav-page {
          display: block; width: 100%;
          padding: 5px 16px 5px 38px;
          background: none; border: none; cursor: pointer;
          font-size: 13px; color: #6b7280; text-align: left;
          transition: background 0.1s, color 0.1s;
          border-left: 2px solid transparent;
        }
        .help-nav-page:hover { background: #e2e8f0; color: #1f2937; }
        .help-nav-page.active {
          color: #1d4ed8; font-weight: 500;
          border-left-color: #3b82f6;
          background: #eff6ff;
        }

        .help-content {
          flex: 1; overflow-y: auto;
          padding: 28px 32px;
        }

        .help-h1 {
          font-size: 22px; font-weight: 700; color: #111827;
          margin: 0 0 20px; line-height: 1.3;
          padding-bottom: 12px; border-bottom: 2px solid #e5e7eb;
        }
        .help-h2 {
          font-size: 16px; font-weight: 600; color: #1f2937;
          margin: 24px 0 10px;
        }
        .help-h3 {
          font-size: 14px; font-weight: 600; color: #374151;
          margin: 18px 0 8px;
        }
        .help-p {
          font-size: 14px; line-height: 1.7; color: #374151;
          margin: 0 0 12px;
        }
        .help-hr {
          border: none; border-top: 1px solid #e5e7eb;
          margin: 20px 0;
        }
        .help-list {
          font-size: 14px; line-height: 1.7; color: #374151;
          margin: 0 0 14px; padding-left: 22px;
        }
        .help-list li { margin-bottom: 4px; }
        .help-code {
          font-family: 'SF Mono', 'Fira Code', monospace;
          font-size: 12px; background: #f1f5f9;
          border: 1px solid #e2e8f0; border-radius: 3px;
          padding: 1px 5px; color: #0f172a;
        }
        .help-table-wrap { overflow-x: auto; margin: 0 0 16px; }
        .help-table {
          width: 100%; border-collapse: collapse;
          font-size: 13px; color: #374151;
        }
        .help-table th {
          background: #f1f5f9; text-align: left;
          padding: 8px 12px; border: 1px solid #e2e8f0;
          font-weight: 600; font-size: 12px; color: #1f2937;
        }
        .help-table td {
          padding: 7px 12px; border: 1px solid #e2e8f0;
          vertical-align: top;
        }
        .help-table tr:nth-child(even) td { background: #f8fafc; }

        /* Search results */
        .help-search-result {
          padding: 14px 16px; border: 1px solid #e5e7eb;
          border-radius: 8px; margin-bottom: 10px; cursor: pointer;
          transition: border-color 0.15s, box-shadow 0.15s;
        }
        .help-search-result:hover {
          border-color: #3b82f6;
          box-shadow: 0 2px 8px rgba(59,130,246,0.1);
        }
        .help-search-result-meta {
          font-size: 11px; color: #6b7280; text-transform: uppercase;
          letter-spacing: 0.06em; margin-bottom: 3px;
        }
        .help-search-result-title {
          font-size: 14px; font-weight: 600; color: #1d4ed8; margin-bottom: 4px;
        }
        .help-search-result-excerpt { font-size: 13px; color: #6b7280; line-height: 1.5; }

        /* Scrollbar styling */
        .help-nav::-webkit-scrollbar,
        .help-content::-webkit-scrollbar { width: 5px; }
        .help-nav::-webkit-scrollbar-track,
        .help-content::-webkit-scrollbar-track { background: transparent; }
        .help-nav::-webkit-scrollbar-thumb,
        .help-content::-webkit-scrollbar-thumb { background: #cbd5e1; border-radius: 3px; }
      `}</style>
    </>
  );
}
