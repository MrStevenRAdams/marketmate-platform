import { lazy, Suspense } from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import Layout from './components/Layout';

// ── Auth / Public pages (outside Layout — no auth guard) ──────────────────────
// These routes must live OUTSIDE the <Layout> wrapper because Layout redirects
// unauthenticated users to /login. If /login itself were inside Layout it would
// cause an infinite redirect loop → blank page with no console errors.
const LoginPage        = lazy(() => import('./pages/Login'));
const RegisterPage     = lazy(() => import('./pages/Register'));
const AcceptInvitePage = lazy(() => import('./pages/AcceptInvite'));
const SetupWizard      = lazy(() => import('./pages/SetupWizard'));
const TemuWizard       = lazy(() => import('./pages/TemuWizard'));

// ── Dashboard ─────────────────────────────────────────────────────────────────
const Dashboard             = lazy(() => import('./pages/Dashboard'));

// ── Catalog ───────────────────────────────────────────────────────────────────
const ProductList           = lazy(() => import('./pages/ProductList'));
const ProductCreate         = lazy(() => import('./pages/ProductCreate'));
const CategoryList          = lazy(() => import('./pages/CategoryList'));
const VariantList           = lazy(() => import('./pages/VariantList'));
const AttributesPage        = lazy(() => import('./pages/AttributesPage'));

// ── Marketplace ───────────────────────────────────────────────────────────────
const MarketplaceConnections   = lazy(() => import('./pages/marketplace/MarketplaceConnections'));
const ChannelConfig            = lazy(() => import('./pages/marketplace/ChannelConfig'));
const ImportDashboard          = lazy(() => import('./pages/marketplace/ImportDashboard'));
const ImportJobDetail          = lazy(() => import('./pages/marketplace/ImportJobDetail'));
const ReviewMatches            = lazy(() => import('./pages/marketplace/ReviewMatches'));
const ListingList              = lazy(() => import('./pages/marketplace/ListingList'));
const ListingCreate            = lazy(() => import('./pages/marketplace/ListingCreate'));
const ListingDetail            = lazy(() => import('./pages/marketplace/ListingDetail'));
const FBAInbound               = lazy(() => import('./pages/marketplace/FBAInbound'));
const AmazonSchemaManager      = lazy(() => import('./pages/marketplace/AmazonSchemaManager'));
const SchemaCacheManager       = lazy(() => import('./pages/marketplace/SchemaCacheManager'));
const JobMonitor               = lazy(() => import('./pages/marketplace/JobMonitor'));
const AmazonListingCreate      = lazy(() => import('./pages/marketplace/AmazonListingCreate'));
const EbayListingCreate        = lazy(() => import('./pages/marketplace/EbayListingCreate'));
const ShopifyListingCreate     = lazy(() => import('./pages/marketplace/ShopifyListingCreate'));
const TemuListingCreate        = lazy(() => import('./pages/marketplace/TemuListingCreate'));
const TikTokListingCreate      = lazy(() => import('./pages/marketplace/TikTokListingCreate'));
const EtsyListingCreate        = lazy(() => import('./pages/marketplace/EtsyListingCreate'));
const WooCommerceListingCreate = lazy(() => import('./pages/marketplace/WooCommerceListingCreate'));
const WalmartListingCreate     = lazy(() => import('./pages/marketplace/WalmartListingCreate'));
const KauflandListingCreate    = lazy(() => import('./pages/marketplace/KauflandListingCreate'));
const MagentoListingCreate     = lazy(() => import('./pages/marketplace/MagentoListingCreate'));
const BigCommerceListingCreate = lazy(() => import('./pages/marketplace/BigCommerceListingCreate'));
const OnBuyListingCreate       = lazy(() => import('./pages/marketplace/OnBuyListingCreate'));
const BlueparkListingCreate    = lazy(() => import('./pages/marketplace/BlueparkListingCreate'));
const WishListingCreate        = lazy(() => import('./pages/marketplace/WishListingCreate'));
const ExtractInventory         = lazy(() => import('./pages/marketplace/ExtractInventory'));
const ConfiguratorList         = lazy(() => import('./pages/marketplace/ConfiguratorList'));
const ConfiguratorDetail       = lazy(() => import('./pages/marketplace/ConfiguratorDetail'));
const ConfiguratorAISetup      = lazy(() => import('./pages/marketplace/ConfiguratorAISetup'));

// ── Operations ────────────────────────────────────────────────────────────────
const Messages             = lazy(() => import('./pages/Messages'));
const Orders               = lazy(() => import('./pages/Orders'));
const ProcessedOrders      = lazy(() => import('./pages/ProcessedOrders'));
const RMAs                 = lazy(() => import('./pages/RMAs'));
const RMADetail            = lazy(() => import('./pages/RMADetail'));
const Dispatch             = lazy(() => import('./components/Dispatch'));
const DespatchConsole      = lazy(() => import('./pages/DespatchConsole'));
const PurchaseOrders       = lazy(() => import('./pages/PurchaseOrders'));
const StockCount           = lazy(() => import('./pages/StockCount'));
const StockScrap           = lazy(() => import('./pages/StockScrap'));
const StockIn              = lazy(() => import('./pages/StockIn'));
const AutomationLogs       = lazy(() => import('./pages/AutomationLogs'));
const WarehouseTransfers   = lazy(() => import('./pages/WarehouseTransfers'));
const PickingReplenishment = lazy(() => import('./pages/PickingReplenishment'));
const VendorOrders         = lazy(() => import('./pages/VendorOrders'));

// ── Inventory ─────────────────────────────────────────────────────────────────
const WarehouseLocations  = lazy(() => import('./pages/WarehouseLocations'));
const StorageGroups       = lazy(() => import('./pages/StorageGroups'));
const FulfilmentSources   = lazy(() => import('./pages/FulfilmentSources'));

// ── Fulfilment ────────────────────────────────────────────────────────────────
const Workflows             = lazy(() => import('./pages/Workflows'));
const Forecasting           = lazy(() => import('./pages/Forecasting'));
const ForecastReplenishment = lazy(() => import('./pages/ForecastReplenishment'));
const Suppliers             = lazy(() => import('./pages/Suppliers'));
const ImportExport          = lazy(() => import('./pages/ImportExport'));
const PostageDefinitions    = lazy(() => import('./pages/PostageDefinitions'));
const LabelPrinting         = lazy(() => import('./pages/LabelPrinting'));

// ── Channels ──────────────────────────────────────────────────────────────────
const PriceSyncPage   = lazy(() => import('./pages/PriceSyncPage'));
const AutomationRules = lazy(() => import('./pages/AutomationRules'));

// ── Analytics ─────────────────────────────────────────────────────────────────
const Analytics          = lazy(() => import('./pages/Analytics'));
const Reports            = lazy(() => import('./pages/Reports'));
const InventoryDashboard = lazy(() => import('./pages/InventoryDashboard'));
const OrderDashboard     = lazy(() => import('./pages/OrderDashboard'));
const PivotAnalytics     = lazy(() => import('./pages/PivotAnalytics'));
const StockItemHistory   = lazy(() => import('./pages/StockItemHistory'));

// ── Settings ──────────────────────────────────────────────────────────────────
const SettingsHub          = lazy(() => import('./pages/settings/SettingsHub'));
const TeamSettings         = lazy(() => import('./pages/TeamSettings'));
const BillingSettings      = lazy(() => import('./pages/BillingSettings'));
const CarrierSettings      = lazy(() => import('./pages/CarrierSettings'));
const AISettings           = lazy(() => import('./pages/settings/AISettings'));
const ApiKeys              = lazy(() => import('./pages/settings/ApiKeys'));
const CurrencySettings     = lazy(() => import('./pages/settings/CurrencySettings'));
const EmailSettings        = lazy(() => import('./pages/settings/EmailSettings'));
const NotificationSettings = lazy(() => import('./pages/settings/NotificationSettings'));

// ── Dev / Admin ───────────────────────────────────────────────────────────────
const OpsConsole          = lazy(() => import('./pages/OpsConsole'));
const TypesenseManagement = lazy(() => import('./pages/TypesenseManagement'));
const EvriApiTester       = lazy(() => import('./pages/EvriApiTester'));
const RoyalMailApiTester  = lazy(() => import('./pages/RoyalMailApiTester'));
const WorkflowSimulator   = lazy(() => import('./pages/WorkflowSimulator'));

// ── Page loading fallback ─────────────────────────────────────────────────────
function PageLoader() {
  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh' }}>
      <div className="spinner" />
    </div>
  );
}

function App() {
  return (
    <BrowserRouter>
      <Suspense fallback={<PageLoader />}>
        <Routes>
          {/* ── Public routes (no auth guard) ─────────────────────────────────
              These MUST be declared before the Layout wrapper so React Router
              matches them first. Layout redirects unauthenticated users to
              /login — if /login were inside Layout it would loop forever. */}
          <Route path="/login"         element={<LoginPage />} />
          <Route path="/register"      element={<RegisterPage />} />
          <Route path="/invite/accept" element={<AcceptInvitePage />} />
          <Route path="/setup"         element={<SetupWizard />} />
          <Route path="/temu-wizard"   element={<TemuWizard />} />

          {/* ── Authenticated routes (Layout provides the auth guard) ──────── */}
          <Route element={<Layout />}>
            {/* Default */}
            <Route path="/" element={<Navigate to="/dashboard" replace />} />

            {/* ── Dashboard ────────────────────────────────────────────────── */}
            <Route path="/dashboard" element={<Dashboard />} />

            {/* ── Catalog ──────────────────────────────────────────────────── */}
            <Route path="/products" element={<ProductList />} />
            <Route path="/products/create" element={<ProductCreate />} />
            <Route path="/products/:id/edit" element={<ProductCreate />} />
            <Route path="/products/:id" element={<ProductCreate />} />
            <Route path="/categories" element={<CategoryList />} />
            <Route path="/attributes" element={<AttributesPage />} />
            <Route path="/variants" element={<VariantList />} />

            {/* ── Marketplace ──────────────────────────────────────────────── */}
            <Route path="/marketplace/connections" element={<MarketplaceConnections />} />
            <Route path="/marketplace/import" element={<ImportDashboard />} />
            <Route path="/marketplace/import/:jobId" element={<ImportJobDetail />} />
            <Route path="/marketplace/import/:jobId/review-matches" element={<ReviewMatches />} />
            <Route path="/marketplace/listings" element={<ListingList />} />
            <Route path="/marketplace/listings/create" element={<ListingCreate />} />
            <Route path="/marketplace/channels/:id/config" element={<ChannelConfig />} />
            <Route path="/marketplace/listings/:id" element={<ListingDetail />} />
            <Route path="/marketplace/configurators" element={<ConfiguratorList />} />
            <Route path="/marketplace/configurators/ai-setup" element={<ConfiguratorAISetup />} />
            <Route path="/marketplace/configurators/:id" element={<ConfiguratorDetail />} />
            <Route path="/marketplace/fba-inbound" element={<FBAInbound />} />
            <Route path="/marketplace/amazon/schemas" element={<AmazonSchemaManager />} />
            <Route path="/marketplace/amazon/listings/create" element={<AmazonListingCreate />} />
            <Route path="/marketplace/ebay/listings/create" element={<EbayListingCreate />} />
            <Route path="/marketplace/shopify/listings/create" element={<ShopifyListingCreate />} />
            <Route path="/marketplace/temu/listings/create" element={<TemuListingCreate />} />
            <Route path="/marketplace/tiktok/listings/create" element={<TikTokListingCreate />} />
            <Route path="/marketplace/etsy/listings/create" element={<EtsyListingCreate />} />
            <Route path="/marketplace/woocommerce/listings/create" element={<WooCommerceListingCreate />} />
            <Route path="/marketplace/walmart/listings/create" element={<WalmartListingCreate />} />
            <Route path="/marketplace/kaufland/listings/create" element={<KauflandListingCreate />} />
            <Route path="/marketplace/magento/listings/create" element={<MagentoListingCreate />} />
            <Route path="/marketplace/bigcommerce/listings/create" element={<BigCommerceListingCreate />} />
            <Route path="/marketplace/onbuy/listings/create" element={<OnBuyListingCreate />} />
            <Route path="/marketplace/bluepark/listings/create" element={<BlueparkListingCreate />} />
            <Route path="/marketplace/wish/listings/create" element={<WishListingCreate />} />
            <Route path="/marketplace/extract" element={<ExtractInventory />} />

            {/* ── Operations ───────────────────────────────────────────────── */}
            <Route path="/messages" element={<Messages />} />
            <Route path="/orders" element={<Orders />} />
            <Route path="/orders/processed" element={<ProcessedOrders />} />
            <Route path="/rmas" element={<RMAs />} />
            <Route path="/rmas/:id" element={<RMADetail />} />
            <Route path="/dispatch" element={<Dispatch />} />
            <Route path="/dispatch/console" element={<DespatchConsole />} />
            <Route path="/dispatch/label-printing" element={<LabelPrinting />} />
            <Route path="/purchase-orders" element={<PurchaseOrders />} />
            <Route path="/stock-count" element={<StockCount />} />
            <Route path="/stock-scrap" element={<StockScrap />} />
            <Route path="/stock-in" element={<StockIn />} />
            <Route path="/automation-logs" element={<AutomationLogs />} />
            <Route path="/warehouse-transfers" element={<WarehouseTransfers />} />
            <Route path="/picking-replenishment" element={<PickingReplenishment />} />
            <Route path="/vendor-orders" element={<VendorOrders />} />

            {/* ── Inventory ────────────────────────────────────────────────── */}
            <Route path="/inventory" element={<WarehouseLocations />} />
            <Route path="/warehouse-locations" element={<WarehouseLocations />} />
            <Route path="/storage-groups" element={<StorageGroups />} />
            <Route path="/fulfilment-sources" element={<FulfilmentSources />} />

            {/* ── Fulfilment ────────────────────────────────────────────────── */}
            <Route path="/workflows" element={<Workflows />} />
            <Route path="/forecasting" element={<Forecasting />} />
            <Route path="/replenishment" element={<ForecastReplenishment />} />
            <Route path="/suppliers" element={<Suppliers />} />
            <Route path="/import-export" element={<ImportExport />} />
            <Route path="/postage-definitions" element={<PostageDefinitions />} />

            {/* ── Channels ─────────────────────────────────────────────────── */}
            <Route path="/price-sync" element={<PriceSyncPage />} />
            <Route path="/automation-rules" element={<AutomationRules />} />

            {/* ── Analytics ────────────────────────────────────────────────── */}
            <Route path="/analytics" element={<Analytics />} />
            <Route path="/reports" element={<Reports />} />
            <Route path="/analytics/inventory" element={<InventoryDashboard />} />
            <Route path="/analytics/orders" element={<OrderDashboard />} />
            <Route path="/analytics/pivot" element={<PivotAnalytics />} />
            <Route path="/stock-history/:productId" element={<StockItemHistory />} />

            {/* ── Settings ─────────────────────────────────────────────────── */}
            <Route path="/settings" element={<SettingsHub />} />
            <Route path="/settings/team" element={<TeamSettings />} />
            <Route path="/settings/billing" element={<BillingSettings />} />
            <Route path="/settings/carriers" element={<CarrierSettings />} />
            <Route path="/settings/ai" element={<AISettings />} />
            <Route path="/settings/api-keys" element={<ApiKeys />} />
            <Route path="/settings/currency" element={<CurrencySettings />} />
            <Route path="/settings/email" element={<EmailSettings />} />
            <Route path="/settings/notifications" element={<NotificationSettings />} />

            {/* ── Dev / Admin ───────────────────────────────────────────────── */}
            <Route path="/ops" element={<OpsConsole />} />
            <Route path="/dev/jobs" element={<JobMonitor />} />
            <Route path="/dev/typesense" element={<TypesenseManagement />} />
            <Route path="/dev/evri" element={<EvriApiTester />} />
            <Route path="/dev/royal-mail" element={<RoyalMailApiTester />} />
            <Route path="/admin/schema-cache" element={<SchemaCacheManager />} />
            <Route path="/workflow-simulator" element={<WorkflowSimulator />} />

            {/* Catch-all — unknown paths fall back to dashboard */}
            <Route path="*" element={<Navigate to="/dashboard" replace />} />
          </Route>
        </Routes>
      </Suspense>
    </BrowserRouter>
  );
}

export default App;
