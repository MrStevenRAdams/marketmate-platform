// ============================================================================
// LISTING EDIT — smart redirect
// ============================================================================
// Route: /marketplace/listings/:id/edit
//
// Fetches the listing record by ID, determines its channel, and redirects to
// the appropriate channel-specific create/edit page (e.g. TemuListingCreate)
// with the correct query params.  This ensures the Edit Listing context-menu
// action (ListingList.tsx line 1107) lands on the same rich form the user
// uses to create a listing, not on the read-only ListingDetail view.
//
// Channel → create page mapping:
//   temu        → /marketplace/temu/listings/create?product_id=&credential_id=
//   amazon      → /marketplace/amazon/listings/create?product_id=
//   ebay        → /marketplace/ebay/listings/create?product_id=
//   shopify     → /marketplace/shopify/listings/create?product_id=
//   shopline    → /marketplace/shopline/listings/create?product_id=
//   tiktok      → /marketplace/tiktok/listings/create?product_id=
//   etsy        → /marketplace/etsy/listings/create?product_id=
//   woocommerce → /marketplace/woocommerce/listings/create?product_id=
//   walmart     → /marketplace/walmart/listings/create?product_id=
//   kaufland    → /marketplace/kaufland/listings/create?product_id=
//   magento     → /marketplace/magento/listings/create?product_id=
//   bigcommerce → /marketplace/bigcommerce/listings/create?product_id=
//   onbuy       → /marketplace/onbuy/listings/create?product_id=
//   bluepark    → /marketplace/bluepark/listings/create?product_id=
//   wish        → /marketplace/wish/listings/create?product_id=
//   backmarket  → /marketplace/backmarket/listings/create?product_id=
//   zalando     → /marketplace/zalando/listings/create?product_id=
//   bol         → /marketplace/bol/listings/create?product_id=
//   lazada      → /marketplace/lazada/listings/create?product_id=
//   (fallback)  → /marketplace/listings/:id  (ListingDetail)

import { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { listingService } from '../../services/marketplace-api';

/** Map a channel name to its create/edit route prefix. */
function channelEditPath(channel: string): string | null {
  const map: Record<string, string> = {
    temu:        '/marketplace/temu/listings/create',
    amazon:      '/marketplace/amazon/listings/create',
    ebay:        '/marketplace/ebay/listings/create',
    shopify:     '/marketplace/shopify/listings/create',
    shopline:    '/marketplace/shopline/listings/create',
    tiktok:      '/marketplace/tiktok/listings/create',
    etsy:        '/marketplace/etsy/listings/create',
    woocommerce: '/marketplace/woocommerce/listings/create',
    walmart:     '/marketplace/walmart/listings/create',
    kaufland:    '/marketplace/kaufland/listings/create',
    magento:     '/marketplace/magento/listings/create',
    bigcommerce: '/marketplace/bigcommerce/listings/create',
    onbuy:       '/marketplace/onbuy/listings/create',
    bluepark:    '/marketplace/bluepark/listings/create',
    wish:        '/marketplace/wish/listings/create',
    backmarket:  '/marketplace/backmarket/listings/create',
    zalando:     '/marketplace/zalando/listings/create',
    bol:         '/marketplace/bol/listings/create',
    lazada:      '/marketplace/lazada/listings/create',
  };
  return map[channel?.toLowerCase()] ?? null;
}

export default function ListingEdit() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [error, setError] = useState('');

  useEffect(() => {
    if (!id) {
      navigate('/marketplace/listings', { replace: true });
      return;
    }
    redirect(id);
  }, [id]);

  async function redirect(listingId: string) {
    try {
      const res = await listingService.getDetail(listingId);
      const listing = res.data?.listing ?? res.data?.data?.listing;

      if (!listing) {
        // Listing not found — fall back to the detail page
        navigate(`/marketplace/listings/${listingId}`, { replace: true });
        return;
      }

      const channel   = listing.channel ?? '';
      const productId = listing.product_id ?? '';
      const credentialId =
        listing.channel_account_id ??
        listing.credential_id ??
        '';

      const basePath = channelEditPath(channel);

      if (!basePath) {
        // No dedicated edit form for this channel — fall back to detail view
        navigate(`/marketplace/listings/${listingId}`, { replace: true });
        return;
      }

      // Build the query string.  product_id is always required; credential_id
      // is optional but passed when available so the channel form can pre-select
      // the correct marketplace account.
      const params = new URLSearchParams();
      if (productId)    params.set('product_id',    productId);
      if (credentialId) params.set('credential_id', credentialId);
      // Pass listing_id so the channel form can load existing data from Firestore
      // instead of generating a fresh AI draft
      params.set('listing_id', listingId);

      navigate(`${basePath}?${params.toString()}`, { replace: true });
    } catch (err: any) {
      console.error('[ListingEdit] Failed to load listing:', err);
      setError(err?.response?.data?.error || err?.message || 'Failed to load listing');
    }
  }

  if (error) {
    return (
      <div style={{ maxWidth: 600, margin: '60px auto', padding: '0 16px', textAlign: 'center' }}>
        <div style={{ fontSize: 40, marginBottom: 16 }}>⚠️</div>
        <h2 style={{ fontSize: 18, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 8 }}>
          Could not open listing for editing
        </h2>
        <p style={{ fontSize: 14, color: 'var(--text-secondary)', marginBottom: 24 }}>{error}</p>
        <button
          onClick={() => navigate('/marketplace/listings')}
          style={{ padding: '10px 24px', borderRadius: 8, border: '1px solid var(--border)', background: 'var(--bg-secondary)', color: 'var(--text-primary)', fontSize: 14, cursor: 'pointer', fontWeight: 600 }}
        >
          ← Back to Listings
        </button>
      </div>
    );
  }

  // Loading state while we fetch and redirect
  return (
    <div style={{ maxWidth: 600, margin: '60px auto', padding: '0 16px', textAlign: 'center' }}>
      <div className="spinner" style={{ margin: '0 auto 16px', width: 32, height: 32, border: '3px solid var(--border)', borderTopColor: 'var(--accent)', borderRadius: '50%', animation: 'spin 0.8s linear infinite' }} />
      <p style={{ color: 'var(--text-secondary)', fontSize: 14 }}>Opening listing editor…</p>
    </div>
  );
}
