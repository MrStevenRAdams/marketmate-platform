package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	_ "module-a/adapters/keyword" // register all keyword transfer adapters via init()
	"module-a/handlers"
	"module-a/marketplace"
	"module-a/marketplace/adapters"
	"module-a/middleware"
	"module-a/repository"
	"module-a/services"
)

func init() {
	// Register marketplace adapters
	marketplace.Register("amazon", adapters.NewAmazonAdapter, marketplace.AdapterMetadata{
		ID:               "amazon",
		Name:             "amazon",
		DisplayName:      "Amazon",
		Icon:             "ri-amazon-fill",
		Color:            "text-orange-600",
		RequiresOAuth:    true,
		SupportedRegions: []string{"US", "UK", "CA", "DE", "FR", "IT", "ES", "JP"},
		Features:         []string{"import", "listing", "fba", "variations"},
		IsActive:         true,
	})

	marketplace.Register("temu", adapters.NewTemuAdapter, marketplace.AdapterMetadata{
		ID:            "temu",
		Name:          "temu",
		DisplayName:   "Temu",
		Icon:          "ri-store-2-fill",
		Color:         "text-orange-500",
		RequiresOAuth: false,
		Features:      []string{"import", "listing"},
		IsActive:      true,
	})

	marketplace.Register("ebay", adapters.NewEbayAdapter, marketplace.AdapterMetadata{
		ID:               "ebay",
		Name:             "ebay",
		DisplayName:      "eBay",
		Icon:             "ri-auction-fill",
		Color:            "text-blue-600",
		RequiresOAuth:    true,
		SupportedRegions: []string{"UK", "US", "DE", "AU", "CA", "FR", "IT", "ES"},
		Features:         []string{"import", "listing", "order_sync", "tracking"},
		IsActive:         true,
	})
	marketplace.Register("shopify", adapters.NewShopifyAdapter, marketplace.AdapterMetadata{
		ID:               "shopify",
		Name:             "shopify",
		DisplayName:      "Shopify",
		Icon:             "ri-shopping-cart-fill",
		Color:            "text-green-600",
		RequiresOAuth:    true,
		SupportedRegions: []string{"GLOBAL"},
		Features:         []string{"import", "listing", "order_sync", "inventory_sync", "price_sync", "tracking"},
		IsActive:         true,
	})
	marketplace.Register("shopline", adapters.NewShoplineAdapter, marketplace.AdapterMetadata{
		ID:               "shopline",
		Name:             "shopline",
		DisplayName:      "Shopline",
		Icon:             "ri-shopping-cart-2-fill",
		Color:            "text-teal-500",
		RequiresOAuth:    true,
		SupportedRegions: []string{"GLOBAL"},
		Features:         []string{"import", "listing", "order_sync", "inventory_sync", "price_sync", "tracking"},
		IsActive:         true,
	})
	marketplace.Register("tiktok", adapters.NewTikTokAdapter, marketplace.AdapterMetadata{
		ID:               "tiktok",
		Name:             "tiktok",
		DisplayName:      "TikTok Shop",
		Icon:             "ri-tiktok-fill",
		Color:            "text-black dark:text-white",
		RequiresOAuth:    true,
		SupportedRegions: []string{"UK", "US", "SEA"},
		Features:         []string{"import", "listing", "order_sync", "tracking", "inventory_sync", "price_sync"},
		IsActive:         true,
	})
	marketplace.Register("etsy", adapters.NewEtsyAdapter, marketplace.AdapterMetadata{
		ID:               "etsy",
		Name:             "etsy",
		DisplayName:      "Etsy",
		Icon:             "ri-store-2-fill",
		Color:            "text-orange-500",
		RequiresOAuth:    true,
		SupportedRegions: []string{"GLOBAL"},
		Features:         []string{"import", "listing", "order_sync", "tracking"},
		IsActive:         true,
	})

	marketplace.Register("woocommerce", adapters.NewWooCommerceAdapter, marketplace.AdapterMetadata{
		ID:               "woocommerce",
		Name:             "woocommerce",
		DisplayName:      "WooCommerce",
		Icon:             "ri-store-3-fill",
		Color:            "text-purple-600",
		RequiresOAuth:    false,
		SupportedRegions: []string{"GLOBAL"},
		Features:         []string{"import", "listing", "order_sync", "inventory_sync", "price_sync", "tracking"},
		IsActive:         true,
	})
	marketplace.Register("shopwired", adapters.NewShopWiredAdapter, marketplace.AdapterMetadata{
		ID:               "shopwired",
		Name:             "shopwired",
		DisplayName:      "ShopWired",
		Icon:             "ri-shopping-cart-2-fill",
		Color:            "text-orange-500",
		RequiresOAuth:    false,
		SupportedRegions: []string{"GB"},
		Features:         []string{"listing", "order_sync", "inventory_sync", "price_sync", "tracking"},
		IsActive:         true,
	})

	marketplace.Register("walmart", adapters.NewWalmartAdapter, marketplace.AdapterMetadata{
		ID:               "walmart",
		Name:             "walmart",
		DisplayName:      "Walmart Marketplace",
		Icon:             "ri-store-2-fill",
		Color:            "text-blue-500",
		RequiresOAuth:    false,
		SupportedRegions: []string{"US"},
		Features:         []string{"import", "listing", "order_sync", "inventory_sync", "price_sync", "tracking"},
		IsActive:         true,
	})

	marketplace.Register("kaufland", adapters.NewKauflandAdapter, marketplace.AdapterMetadata{
		ID:               "kaufland",
		Name:             "kaufland",
		DisplayName:      "Kaufland",
		Icon:             "ri-shopping-bag-3-fill",
		Color:            "text-red-600",
		RequiresOAuth:    false,
		SupportedRegions: []string{"DE", "SK", "CZ", "PL", "HR", "RO", "BG"},
		Features:         []string{"import", "listing", "order_sync", "inventory_sync", "price_sync", "tracking"},
		IsActive:         true,
	})

	marketplace.Register("magento", adapters.NewMagentoAdapter, marketplace.AdapterMetadata{
		ID:               "magento",
		Name:             "magento",
		DisplayName:      "Magento 2",
		Icon:             "ri-store-line",
		Color:            "text-orange-500",
		RequiresOAuth:    false,
		SupportedRegions: []string{"GLOBAL"},
		Features:         []string{"import", "listing", "order_sync", "inventory_sync", "price_sync", "tracking"},
		IsActive:         true,
	})

	marketplace.Register("bigcommerce", adapters.NewBigCommerceAdapter, marketplace.AdapterMetadata{
		ID:               "bigcommerce",
		Name:             "bigcommerce",
		DisplayName:      "BigCommerce",
		Icon:             "ri-store-3-line",
		Color:            "text-blue-600",
		RequiresOAuth:    false,
		SupportedRegions: []string{"GLOBAL"},
		Features:         []string{"import", "listing", "order_sync", "inventory_sync", "price_sync", "tracking"},
		IsActive:         true,
	})

	marketplace.Register("onbuy", adapters.NewOnBuyAdapter, marketplace.AdapterMetadata{
		ID:               "onbuy",
		Name:             "onbuy",
		DisplayName:      "OnBuy",
		Icon:             "ri-shopping-bag-3-line",
		Color:            "text-orange-500",
		RequiresOAuth:    false,
		SupportedRegions: []string{"GB"},
		Features:         []string{"import", "listing", "order_sync", "inventory_sync", "price_sync", "tracking"},
		IsActive:         true,
	})

	// ── SESSION 4 CHANNELS ─────────────────────────────────────────────────
	marketplace.Register("backmarket", adapters.NewBackMarketAdapter, marketplace.AdapterMetadata{
		ID:               "backmarket",
		Name:             "backmarket",
		DisplayName:      "Back Market",
		Icon:             "ri-recycle-fill",
		Color:            "text-teal-600",
		RequiresOAuth:    false,
		SupportedRegions: []string{"FR", "DE", "GB", "US", "ES", "IT", "BE", "NL", "AT", "JP", "AU"},
		Features:         []string{"listing", "order_sync", "tracking", "inventory_sync", "price_sync"},
		IsActive:         true,
	})
	marketplace.Register("zalando", adapters.NewZalandoAdapter, marketplace.AdapterMetadata{
		ID:               "zalando",
		Name:             "zalando",
		DisplayName:      "Zalando",
		Icon:             "ri-shopping-bag-3-fill",
		Color:            "text-orange-500",
		RequiresOAuth:    false,
		SupportedRegions: []string{"DE", "AT", "CH", "FR", "IT", "NL", "PL", "BE", "GB", "SE", "DK", "FI", "NO"},
		Features:         []string{"listing", "order_sync", "tracking", "inventory_sync", "price_sync"},
		IsActive:         true,
	})
	marketplace.Register("bol", adapters.NewBolAdapter, marketplace.AdapterMetadata{
		ID:               "bol",
		Name:             "bol",
		DisplayName:      "Bol.com",
		Icon:             "ri-store-2-fill",
		Color:            "text-blue-700",
		RequiresOAuth:    false,
		SupportedRegions: []string{"NL", "BE"},
		Features:         []string{"listing", "order_sync", "tracking", "inventory_sync", "price_sync"},
		IsActive:         true,
	})
	marketplace.Register("lazada", adapters.NewLazadaAdapter, marketplace.AdapterMetadata{
		ID:               "lazada",
		Name:             "lazada",
		DisplayName:      "Lazada",
		Icon:             "ri-store-fill",
		Color:            "text-blue-500",
		RequiresOAuth:    false,
		SupportedRegions: []string{"MY", "SG", "TH", "ID", "PH", "VN"},
		Features:         []string{"listing", "order_sync", "tracking", "inventory_sync", "price_sync"},
		IsActive:         true,
	})

	// ── MIRAKL-POWERED MARKETPLACES ────────────────────────────────────────
	// All use the generic Mirakl Seller API. Each marketplace requires its own
	// api_key and instance URL (base_url). Credentials stored per-tenant.
	// See: https://developer.mirakl.com/content/product/mmp/rest/seller/openapi3

	miraklFeatures := []string{"listing", "order_sync", "tracking", "inventory_sync", "price_sync", "bulk_update", "cancellation", "refunds"}

	// UK
	marketplace.Register("tesco", adapters.NewTescoAdapter, marketplace.AdapterMetadata{
		ID: "tesco", Name: "tesco", DisplayName: "Tesco Marketplace",
		Icon: "ri-store-3-fill", Color: "text-blue-600",
		RequiresOAuth: false, Features: miraklFeatures, IsActive: true,
		SupportedRegions: []string{"GB"},
	})
	marketplace.Register("bandq", adapters.NewBandQAdapter, marketplace.AdapterMetadata{
		ID: "bandq", Name: "bandq", DisplayName: "B&Q Marketplace",
		Icon: "ri-tools-fill", Color: "text-orange-500",
		RequiresOAuth: false, Features: miraklFeatures, IsActive: true,
		SupportedRegions: []string{"GB"},
	})
	marketplace.Register("superdrug", adapters.NewSuperdrugAdapter, marketplace.AdapterMetadata{
		ID: "superdrug", Name: "superdrug", DisplayName: "Superdrug Marketplace",
		Icon: "ri-heart-pulse-fill", Color: "text-pink-500",
		RequiresOAuth: false, Features: miraklFeatures, IsActive: true,
		SupportedRegions: []string{"GB"},
	})
	marketplace.Register("debenhams", adapters.NewDebenhamsAdapter, marketplace.AdapterMetadata{
		ID: "debenhams", Name: "debenhams", DisplayName: "Debenhams Marketplace",
		Icon: "ri-shirt-fill", Color: "text-purple-600",
		RequiresOAuth: false, Features: miraklFeatures, IsActive: true,
		SupportedRegions: []string{"GB"},
	})
	marketplace.Register("decathlon_uk", adapters.NewDecathlonUKAdapter, marketplace.AdapterMetadata{
		ID: "decathlon_uk", Name: "decathlon_uk", DisplayName: "Decathlon UK",
		Icon: "ri-football-fill", Color: "text-blue-500",
		RequiresOAuth: false, Features: miraklFeatures, IsActive: true,
		SupportedRegions: []string{"GB"},
	})
	marketplace.Register("mountain_warehouse", adapters.NewMountainWarehouseAdapter, marketplace.AdapterMetadata{
		ID: "mountain_warehouse", Name: "mountain_warehouse", DisplayName: "Mountain Warehouse",
		Icon: "ri-landscape-fill", Color: "text-green-700",
		RequiresOAuth: false, Features: miraklFeatures, IsActive: true,
		SupportedRegions: []string{"GB"},
	})
	marketplace.Register("jd_sports", adapters.NewJDSportsAdapter, marketplace.AdapterMetadata{
		ID: "jd_sports", Name: "jd_sports", DisplayName: "JD Sports Marketplace",
		Icon: "ri-run-fill", Color: "text-yellow-500",
		RequiresOAuth: false, Features: miraklFeatures, IsActive: true,
		SupportedRegions: []string{"GB"},
	})
	// EU
	marketplace.Register("carrefour", adapters.NewCarrefourAdapter, marketplace.AdapterMetadata{
		ID: "carrefour", Name: "carrefour", DisplayName: "Carrefour Marketplace",
		Icon: "ri-store-fill", Color: "text-blue-700",
		RequiresOAuth: false, Features: miraklFeatures, IsActive: true,
		SupportedRegions: []string{"FR", "ES", "IT", "PL", "BE"},
	})
	marketplace.Register("decathlon_fr", adapters.NewDecathlonFRAdapter, marketplace.AdapterMetadata{
		ID: "decathlon_fr", Name: "decathlon_fr", DisplayName: "Decathlon France",
		Icon: "ri-football-fill", Color: "text-blue-500",
		RequiresOAuth: false, Features: miraklFeatures, IsActive: true,
		SupportedRegions: []string{"FR"},
	})
	marketplace.Register("fnac_darty", adapters.NewFnacDartyAdapter, marketplace.AdapterMetadata{
		ID: "fnac_darty", Name: "fnac_darty", DisplayName: "Fnac Darty Marketplace",
		Icon: "ri-music-fill", Color: "text-yellow-600",
		RequiresOAuth: false, Features: miraklFeatures, IsActive: true,
		SupportedRegions: []string{"FR", "BE"},
	})
	marketplace.Register("leroy_merlin", adapters.NewLeroyMerlinAdapter, marketplace.AdapterMetadata{
		ID: "leroy_merlin", Name: "leroy_merlin", DisplayName: "Leroy Merlin Marketplace",
		Icon: "ri-hammer-fill", Color: "text-green-600",
		RequiresOAuth: false, Features: miraklFeatures, IsActive: true,
		SupportedRegions: []string{"FR", "ES", "IT", "PL"},
	})
	marketplace.Register("mediamarkt", adapters.NewMediaMarktAdapter, marketplace.AdapterMetadata{
		ID: "mediamarkt", Name: "mediamarkt", DisplayName: "MediaMarkt Marketplace",
		Icon: "ri-tv-fill", Color: "text-red-600",
		RequiresOAuth: false, Features: miraklFeatures, IsActive: true,
		SupportedRegions: []string{"DE", "NL", "ES", "IT", "AT"},
	})
	// Global
	marketplace.Register("asos", adapters.NewASOSAdapter, marketplace.AdapterMetadata{
		ID: "asos", Name: "asos", DisplayName: "ASOS Marketplace",
		Icon: "ri-shopping-bag-fill", Color: "text-gray-800",
		RequiresOAuth: false, Features: miraklFeatures, IsActive: true,
		SupportedRegions: []string{"GB", "US", "AU", "FR", "DE"},
	})
	marketplace.Register("macys", adapters.NewMacysAdapter, marketplace.AdapterMetadata{
		ID: "macys", Name: "macys", DisplayName: "Macy's Marketplace",
		Icon: "ri-star-fill", Color: "text-red-700",
		RequiresOAuth: false, Features: miraklFeatures, IsActive: true,
		SupportedRegions: []string{"US"},
	})
	marketplace.Register("lowes", adapters.NewLowesAdapter, marketplace.AdapterMetadata{
		ID: "lowes", Name: "lowes", DisplayName: "Lowe's Marketplace",
		Icon: "ri-tools-fill", Color: "text-blue-800",
		RequiresOAuth: false, Features: miraklFeatures, IsActive: true,
		SupportedRegions: []string{"US", "CA"},
	})
}

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	// Get configuration
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		projectID = "marketmate-486116"
	}

	gcsBucket := os.Getenv("GCS_BUCKET_NAME")
	if gcsBucket == "" {
		gcsBucket = "marketmate"
	}

	// GCS_CREDENTIALS_FILE is optional — if unset, Application Default Credentials
	// are used automatically (Cloud Run service account on GCP, or
	// `gcloud auth application-default login` credentials locally).
	gcsCredentials := os.Getenv("GCS_CREDENTIALS_FILE")

	encryptionKey := os.Getenv("CREDENTIAL_ENCRYPTION_KEY")
	if encryptionKey == "" {
		encryptionKey = "default-32-char-key-change-me!!"
		log.Println("⚠️  Using default encryption key - set CREDENTIAL_ENCRYPTION_KEY in production!")
	}
	if len(encryptionKey) > 32 {
		log.Printf("⚠️  Encryption key is %d bytes, trimming to 32 for AES-256", len(encryptionKey))
		encryptionKey = encryptionKey[:32]
	} else if len(encryptionKey) < 32 {
		log.Fatalf("❌ Encryption key is only %d bytes, need exactly 32 for AES-256", len(encryptionKey))
	}

	// Initialize Firestore repository
	ctx := context.Background()
	firestoreRepo, err := repository.NewFirestoreRepository(ctx, projectID)
	if err != nil {
		log.Fatalf("Failed to initialize Firestore: %v", err)
	}
	defer firestoreRepo.Close()

	// Initialize marketplace repository
	marketplaceRepo := repository.NewMarketplaceRepository(firestoreRepo.GetClient())

	// Initialize global config repository (company-wide keys)
	globalConfigRepo := repository.NewGlobalConfigRepository(firestoreRepo.GetClient())

	// Seed global marketplace keys from environment variables
	seedGlobalMarketplaceKeys(ctx, globalConfigRepo)

	// Initialize Storage Service
	storageService, err := services.NewStorageService(gcsCredentials, gcsBucket)
	if err != nil {
		log.Println("⚠️  GCS credentials not configured. File uploads will not be available.")
		log.Println("⚠️  Error:", err)
		storageService = nil
	} else {
		log.Println("✅ Storage service initialized successfully")
	}

	// Initialize Module A Services
	productService := services.NewProductService(firestoreRepo)
	attributeService := services.NewAttributeService(firestoreRepo)

	// Initialize Search (Typesense) — before handlers so it can be injected
	searchService := services.NewSearchService(firestoreRepo)
	log.Printf("🔍 Typesense connecting to %s...", os.Getenv("TYPESENSE_URL"))
	if searchService.Healthy() {
		reindexNeeded, err := searchService.EnsureCollections()
		if err != nil {
			log.Printf("⚠️  Typesense collection setup failed: %v", err)
		} else {
			log.Println("✅ Typesense search engine connected")
			if reindexNeeded {
				// Schema changed (or fresh install) — reindex all tenants in background
				// so the server starts accepting requests immediately.
				log.Println("🔄 Typesense schema changed — triggering background reindex of all tenants")
				go func() {
					bgCtx := context.Background()
					iter := firestoreRepo.GetClient().Collection("tenants").Documents(bgCtx)
					defer iter.Stop()
					for {
						doc, err := iter.Next()
						if err != nil {
							break
						}
						tenantID, _ := doc.Data()["tenant_id"].(string)
						if tenantID == "" {
							tenantID = doc.Ref.ID
						}
						n, err := searchService.SyncAllProducts(bgCtx, tenantID)
						if err != nil {
							log.Printf("⚠️  Background reindex failed for tenant %s: %v", tenantID, err)
						} else {
							log.Printf("✅ Background reindex complete for tenant %s: %d products", tenantID, n)
						}
					}
				}()
			}
		}
	} else {
		log.Println("⚠️  Typesense not available — search will be unavailable until it's running")
	}

	// Initialize Module B Services (now with globalConfigRepo)
	marketplaceService := services.NewMarketplaceService(marketplaceRepo, globalConfigRepo, encryptionKey)
	importService := services.NewImportService(marketplaceRepo, productService, marketplaceService)
	importService.SetSearchService(searchService) // wire search so imported products are indexed

	// Wire Cloud Tasks for per-product enrichment queuing on eBay import.
	// Gracefully degraded — if Cloud Tasks isn't available (dev/no creds) the
	// import still works; a bulk enrichment run catches those products later.
	taskSvc, taskSvcErr := services.NewTaskService(ctx)
	if taskSvcErr != nil {
		log.Printf("⚠️  Cloud Tasks unavailable — per-product enrichment queuing disabled: %v", taskSvcErr)
	} else {
		importService.SetTaskService(taskSvc)
		log.Println("✅ Cloud Tasks: per-product eBay enrichment queuing enabled")
	}
	listingService := services.NewListingService(marketplaceRepo, marketplaceService, productService)

	// Initialize Order Service (Module E)
	piiService := services.NewPIIService()
	orderService := services.NewOrderService(firestoreRepo, piiService)
	log.Println("✅ PIIService: field-level PII encryption initialised")
	returnsPortalHandler := handlers.NewReturnsPortalHandler(firestoreRepo.GetClient(), piiService)

	// Initialize AI Service (hybrid: Gemini + Claude)
	aiService := services.NewAIService()
	if aiService.IsAvailable() {
		log.Printf("🤖 AI Service: Ready (mode: %s)", getAIModeStr(aiService))
	} else {
		log.Println("⚠️  AI Service: Not configured (set GEMINI_API_KEY and/or CLAUDE_API_KEY)")
	}

	// Initialize Handlers
	productHandler := handlers.NewProductHandler(productService, searchService, firestoreRepo.GetClient())
	productExtHandler := handlers.NewProductExtHandler(firestoreRepo.GetClient())
	attributeHandler := handlers.NewAttributeHandler(attributeService)
	marketplaceHandler := handlers.NewMarketplaceHandler(marketplaceService, marketplaceRepo, importService, listingService, searchService)
	temuHandler := handlers.NewTemuHandler(marketplaceService, marketplaceRepo, firestoreRepo)
	temuHandler.SetFirestoreClient(firestoreRepo.GetClient()) // wire Firestore schema cache
	ebayHandler := handlers.NewEbayHandler(marketplaceService, marketplaceRepo, firestoreRepo)
	// ── SESSION 5 — SHOPIFY LISTING HANDLER (PRC-01) ─────────────────────────
	shopifyListingHandler := handlers.NewShopifyListingHandler(marketplaceService, marketplaceRepo, firestoreRepo)
	shopifyHandler := handlers.NewShopifyHandler(marketplaceService, marketplaceRepo, orderService, firestoreRepo)
	log.Println("✅ Session 5 (Shopify Listing Handler + Full Handler): Ready")
	// ── SHOPLINE ──────────────────────────────────────────────────────────────
	shoplineListingHandler := handlers.NewShoplineListingHandler(marketplaceService, marketplaceRepo, firestoreRepo)
	shoplineHandler := handlers.NewShoplineHandler(marketplaceService, marketplaceRepo, orderService, firestoreRepo)
	log.Println("✅ Shopline Listing Handler + Full Handler: Ready")
	amazonHandler := handlers.NewAmazonHandler(marketplaceService, marketplaceRepo, firestoreRepo, globalConfigRepo)
	amazonOAuthHandler := handlers.NewAmazonOAuthHandler(marketplaceService, marketplaceRepo)
	amazonSchemaHandler := handlers.NewAmazonSchemaHandler(marketplaceService, marketplaceRepo, firestoreRepo.GetClient())
	ebaySchemaHandler := handlers.NewEbaySchemaHandler(marketplaceService, marketplaceRepo, firestoreRepo.GetClient())
	temuSchemaHandler := handlers.NewTemuSchemaHandler(marketplaceService, marketplaceRepo, firestoreRepo.GetClient())
	systemHandler := handlers.NewSystemHandler()
	tiktokHandler := handlers.NewTikTokHandler(marketplaceService, marketplaceRepo, firestoreRepo)
	tiktokOrdersHandler := handlers.NewTikTokOrdersHandler(orderService, marketplaceService)
	etsyHandler := handlers.NewEtsyHandler(marketplaceService, marketplaceRepo, firestoreRepo)
	etsyOrdersHandler := handlers.NewEtsyOrdersHandler(orderService, marketplaceService)
	wooHandler := handlers.NewWooCommerceHandler(marketplaceService, marketplaceRepo, firestoreRepo)
	wooOrdersHandler := handlers.NewWooCommerceOrdersHandler(orderService, marketplaceService)
	shopwiredHandler := handlers.NewShopWiredHandler(marketplaceService, marketplaceRepo, firestoreRepo)
	shopwiredOrdersHandler := handlers.NewShopWiredOrdersHandler(orderService, marketplaceService)
	walmartHandler := handlers.NewWalmartHandler(marketplaceService, marketplaceRepo, firestoreRepo)
	walmartOrdersHandler := handlers.NewWalmartOrdersHandler(orderService, marketplaceService)
	kauflandHandler := handlers.NewKauflandHandler(marketplaceService, marketplaceRepo, firestoreRepo)
	kauflandOrdersHandler := handlers.NewKauflandOrdersHandler(orderService, marketplaceService)
	magentoHandler := handlers.NewMagentoHandler(marketplaceService, marketplaceRepo, firestoreRepo)
	magentoOrdersHandler := handlers.NewMagentoOrdersHandler(orderService, marketplaceService)
	bigcommerceHandler := handlers.NewBigCommerceHandler(marketplaceService, marketplaceRepo, firestoreRepo)
	bigcommerceOrdersHandler := handlers.NewBigCommerceOrdersHandler(orderService, marketplaceService)
	onbuyHandler := handlers.NewOnBuyHandler(marketplaceService, marketplaceRepo, firestoreRepo)
	onbuyOrdersHandler := handlers.NewOnBuyOrdersHandler(orderService, marketplaceService)
	blueparkHandler := handlers.NewBlueparkHandler(marketplaceService, marketplaceRepo, firestoreRepo, orderService)
	wishHandler := handlers.NewWishHandler(marketplaceService, marketplaceRepo, firestoreRepo, orderService)
	log.Println("✅ Session D (Bluepark + Wish Handlers): Ready")
	extractHandler := handlers.NewExtractHandler(marketplaceService, marketplaceRepo, firestoreRepo, productService)
	log.Println("✅ Session E (Extract Handler): Ready")
	miraklHandler := handlers.NewMiraklHandler(marketplaceService, marketplaceRepo)
	miraklOrdersHandler := handlers.NewMiraklOrdersHandler(orderService, marketplaceService)
	exportService := services.NewExportService(firestoreRepo)
	exportHandler := handlers.NewExportHandler(exportService, firestoreRepo, productService)
	exportHandler.SetOrderService(orderService)
	if storageService != nil {
		exportHandler.SetStorageService(storageService)
	}
	exportHandler.SetFirestoreClient(firestoreRepo.GetClient())
	importHandler := handlers.NewImportHandler(firestoreRepo, productService, firestoreRepo.GetClient())
	// PIM Import/Export — bulk CSV/XLSX product catalogue management (/import-export nav item)
	pimHandler := handlers.NewPIMImportHandler(firestoreRepo, productService, firestoreRepo.GetClient())
	tenantHandler := handlers.NewTenantHandler(firestoreRepo.GetClient())
	aiHandler := handlers.NewAIHandler(aiService, firestoreRepo, marketplaceRepo, productService, listingService)

	// Initialize Order Handlers (channel handlers first, then main handler)
	amazonOrdersHandler := handlers.NewAmazonOrdersHandler(orderService, marketplaceService)
	ebayOrdersHandler := handlers.NewEbayOrdersHandler(orderService, marketplaceService)
	temuOrdersHandler := handlers.NewTemuOrdersHandler(orderService, marketplaceService)
	orderHandler := handlers.NewOrderHandler(orderService, amazonOrdersHandler, ebayOrdersHandler, temuOrdersHandler)
	orderHandler.SetTikTokOrdersHandler(tiktokOrdersHandler)
	orderHandler.SetEtsyOrdersHandler(etsyOrdersHandler)
	orderHandler.SetWooCommerceOrdersHandler(wooOrdersHandler)
	orderHandler.SetWalmartOrdersHandler(walmartOrdersHandler)
	orderHandler.SetKauflandOrdersHandler(kauflandOrdersHandler)
	orderHandler.SetMagentoOrdersHandler(magentoOrdersHandler)
	orderHandler.SetBigCommerceOrdersHandler(bigcommerceOrdersHandler)
	orderHandler.SetOnBuyOrdersHandler(onbuyOrdersHandler)
	// NOTE: Tesco has migrated to Mirakl — use /api/v1/mirakl with credential_id pointing to the Tesco instance

	// ── SESSION 4 — Back Market, Zalando, Bol.com, Lazada ─────────────────
	backmarketOrdersHandler := handlers.NewBackMarketOrdersHandler(orderService, marketplaceService)
	zalandoOrdersHandler := handlers.NewZalandoOrdersHandler(orderService, marketplaceService)
	bolOrdersHandler := handlers.NewBolOrdersHandler(orderService, marketplaceService)
	lazadaOrdersHandler := handlers.NewLazadaOrdersHandler(orderService, marketplaceService)

	// Session 6.2 — S4 Bulk Order Handlers
	backmarketBulkHandler := handlers.NewBackMarketBulkHandler(orderService, marketplaceService)
	zalandoBulkHandler := handlers.NewZalandoBulkHandler(orderService, marketplaceService)
	bolBulkHandler := handlers.NewBolBulkHandler(orderService, marketplaceService)
	lazadaBulkHandler := handlers.NewLazadaBulkHandler(orderService, marketplaceService)
	// packingSlipHandler declared below after templateService is initialised
	reconcileHandler := handlers.NewReconcileHandler(marketplaceService, marketplaceRepo, firestoreRepo, productService, searchService)
	orderHandler.SetBackMarketOrdersHandler(backmarketOrdersHandler)
	orderHandler.SetZalandoOrdersHandler(zalandoOrdersHandler)
	orderHandler.SetBolOrdersHandler(bolOrdersHandler)
	orderHandler.SetLazadaOrdersHandler(lazadaOrdersHandler)
	orderHandler.SetShopifyHandler(shopifyHandler)
	orderHandler.SetShoplineHandler(shoplineHandler)
	orderHandler.SetShopWiredOrdersHandler(shopwiredOrdersHandler)
	log.Println("✅ Session 4 (Back Market, Zalando, Bol.com, Lazada + SKU Reconciliation): Ready")

	// Initialize Orchestrator Handler (manual order download — BUG-001 fix)
	orchestratorHandler := handlers.NewOrchestratorHandler(marketplaceRepo, orderHandler)

	// ── Order sync via Cloud Tasks ─────────────────────────────────────────────
	// Each credential with order sync enabled runs as its own independent
	// self-rescheduling Cloud Tasks chain. taskSvc may be nil in local dev —
	// sync still works via the manual "Download Now" button.
	orderSyncHandler := handlers.NewOrderSyncHandler(marketplaceRepo, orderHandler, taskSvc, firestoreRepo.GetClient())
	marketplaceHandler.SetOrderSyncHandler(orderSyncHandler)
	// Wire Firestore into marketplace handler for the dynamic registry endpoint.
	marketplaceHandler.SetFirestoreClient(firestoreRepo.GetClient())

	// Seed task chains for all already-enabled credentials on startup.
	// This restarts any chains that were lost when the server was last deployed.
	go orderSyncHandler.SeedAllActiveCredentials(ctx)

	// Order webhook handler — receives push notifications from marketplaces
	// that support webhooks (eBay, WooCommerce, Shopify, BigCommerce, TikTok).
	orderWebhookHandler := handlers.NewOrderWebhookHandler(marketplaceRepo, marketplaceService, orderHandler)
	orderWebhookHandler.SetFirestoreClient(firestoreRepo.GetClient())
	orderWebhookHandler.SetMessagingNotifier(services.NewMessagingNotifier(firestoreRepo.GetClient()))

	// Initialize Order Actions Handler (holds, locks, tags, notes)
	orderActionsHandler := handlers.NewOrderActionsHandler(firestoreRepo.GetClient())

	// Initialize Order Actions Extended Handler (Actions flyout — organise, items, shipping, process, other)
	orderActionsExtendedHandler := handlers.NewOrderActionsExtendedHandler(firestoreRepo.GetClient(), orderService)

	// Initialize Inventory Handler (Module D)
	inventoryHandler := handlers.NewInventoryHandler(firestoreRepo.GetClient())

	// Initialize Warehouse Location Handler (Module D extension)
	warehouseLocationHandler := handlers.NewWarehouseLocationHandler(firestoreRepo.GetClient())
	cancellationAlertHandler := handlers.NewCancellationAlertHandler(firestoreRepo.GetClient())

	// Initialize Dispatch Handler (Module F)
	dispatchHandler := handlers.NewDispatchHandler(firestoreRepo.GetClient())
	if storageService != nil {
		dispatchHandler.SetStorageService(storageService)
	}

	// Initialize Dispatch Extensions Handler (Session 3)
	dispatchExtHandler := handlers.NewDispatchExtensionsHandler(firestoreRepo.GetClient())

	// Initialize Evri tracking sync scheduler — polls every 15 min for all active Evri shipments
	trackingSyncHandler := handlers.NewTrackingSyncHandler(firestoreRepo.GetClient())
	trackingSyncHandler.Run() // starts background goroutine

	// Initialize shipping templates + customs profiles handlers
	shippingTemplateHandler := handlers.NewShippingTemplateHandler(firestoreRepo.GetClient())
	customsProfileHandler := handlers.NewCustomsProfileHandler(firestoreRepo.GetClient())

	// Initialize Workflow Engine + Handler (Module G)
	workflowEngine := services.NewWorkflowEngine(firestoreRepo)
	workflowHandler := handlers.NewWorkflowHandler(workflowEngine, firestoreRepo.GetClient())

	// Initialize Fulfilment Source + Supplier Handler (Module G)
	fulfilmentSourceHandler := handlers.NewFulfilmentSourceHandler(firestoreRepo.GetClient())
	supplierHandler := handlers.NewSupplierHandler(firestoreRepo.GetClient())
	purchaseOrderHandler := handlers.NewPurchaseOrderHandler(firestoreRepo.GetClient())
	reorderSuggestionHandler := handlers.NewReorderSuggestionHandler(firestoreRepo.GetClient(), purchaseOrderHandler)
	rmaHandler := handlers.NewRMAHandler(firestoreRepo.GetClient(), marketplaceService)
	refundDownloadHandler := handlers.NewRefundDownloadHandler(firestoreRepo.GetClient(), marketplaceService)
	refundPushHandler := handlers.NewRefundPushHandler(firestoreRepo.GetClient(), marketplaceService)
	stockCountHandler := handlers.NewStockCountHandler(firestoreRepo.GetClient())
	stockScrapHandler := handlers.NewStockScrapHandler(firestoreRepo.GetClient())
	forecastingHandler := handlers.NewForecastingHandler(firestoreRepo.GetClient())
	autoReorderHandler := handlers.NewAutoReorderHandler(forecastingHandler)

	// Session 1 — Navigation & Global UI handlers
	notificationHandler := handlers.NewNotificationHandler(firestoreRepo.GetClient())
	trackingWebhookHandler := handlers.NewTrackingWebhookHandler(firestoreRepo.GetClient(), notificationHandler)
	emailTemplateHandler := handlers.NewEmailTemplateHandler(firestoreRepo.GetClient())
	emailLogHandler := handlers.NewEmailLogHandler(firestoreRepo.GetClient())
	sentMailHandler := handlers.NewSentMailHandler(firestoreRepo.GetClient())
	pickwaveHandler := handlers.NewPickwaveHandler(firestoreRepo.GetClient())

	messagingHandler := handlers.NewMessagingHandler(firestoreRepo.GetClient(), marketplaceService)
	messagingAIHandler := handlers.NewMessagingAIHandler(
		firestoreRepo.GetClient(),
		marketplaceService,
		messagingHandler,
	)
	// Inject AI agent back into messaging handler for auto-processing on sync
	messagingHandler.SetAIAgent(messagingAIHandler)
	amazonMessagingWebhookHandler := handlers.NewAmazonMessagingWebhookHandler(
		firestoreRepo.GetClient(),
		marketplaceService,
		messagingHandler,
	)

	// ── GAP CLOSURE HANDLERS ──────────────────────────────────────────────────
	batchHandler := handlers.NewBatchHandler(firestoreRepo.GetClient())
	fbaInboundHandler := handlers.NewFBAInboundHandler(firestoreRepo.GetClient(), marketplaceRepo, marketplaceService)
	syncStatusHandler := handlers.NewSyncStatusHandler(firestoreRepo.GetClient())
	inventorySyncHandler := handlers.NewInventorySyncHandler(firestoreRepo.GetClient())
	stockReservationHandler := handlers.NewStockReservationHandler(firestoreRepo.GetClient())
	binrackHandler := handlers.NewBinrackHandler(firestoreRepo.GetClient())
	postageDefinitionHandler := handlers.NewPostageDefinitionHandler(firestoreRepo.GetClient())
	vendorOrderHandler := handlers.NewVendorOrderHandler(firestoreRepo.GetClient(), marketplaceRepo, marketplaceService)
	productExtensionsHandler := handlers.NewProductExtensionsHandler(firestoreRepo.GetClient())
	supplierReturnHandler := handlers.NewSupplierReturnHandler(firestoreRepo.GetClient())
	picklistHandler := handlers.NewPicklistHandler(firestoreRepo.GetClient())
	labelPrintingHandler := handlers.NewLabelPrintingHandler(firestoreRepo.GetClient())
	settingsHandler := handlers.NewSettingsHandler(firestoreRepo.GetClient())
	binTypeHandler := handlers.NewBinTypeHandler(firestoreRepo.GetClient())
	scheduleHandler := handlers.NewScheduleHandler(firestoreRepo.GetClient())
	opsHandler := handlers.NewOpsHandler(firestoreRepo.GetClient())
	opsHandler.SetOrderSyncHandler(orderSyncHandler)

	// ── P0 FEATURE HANDLERS ───────────────────────────────────────────────────
	listingDescriptionHandler := handlers.NewListingDescriptionHandler(firestoreRepo.GetClient())
	skuCheckHandler := handlers.NewSKUCheckHandler(firestoreRepo.GetClient()) // FLD-15
	inventoryViewHandler := handlers.NewInventoryViewHandler(firestoreRepo.GetClient())
	automationLogHandler := handlers.NewAutomationLogHandler(firestoreRepo.GetClient())

	// B-007: Saved order views
	orderViewHandler := handlers.NewOrderViewHandler(firestoreRepo.GetClient())

	// B-002/B-003/B-004/B-005/B-006: Order management (manual create, edit, merge, split, cancel)
	orderManagementHandler := handlers.NewOrderManagementHandler(firestoreRepo.GetClient(), orderService, marketplaceService)
	// H-001: In-app changelog
	changelogHandler := handlers.NewChangelogHandler(firestoreRepo.GetClient())
	storageGroupHandler := handlers.NewStorageGroupHandler(firestoreRepo.GetClient())
	priceSyncHandler := handlers.NewPriceSyncHandler(firestoreRepo.GetClient())

	// AI Consolidation + Multi-channel listing generation
	aiConsolidationSvc := services.NewAIConsolidationService(aiService, marketplaceRepo, firestoreRepo)
	aiListingGenSvc    := services.NewAIListingGenerationService(aiService)
	// aiConsolidationHandler is constructed after kwIntelSvc (below)
	log.Println("✅ AI Consolidation Service: Ready")

	// eBay Browse Enrichment
	ebayEnrichService := services.NewEbayEnrichmentService(marketplaceRepo)
	ebayEnrichHandler := handlers.NewEbayEnrichmentHandler(ebayEnrichService, marketplaceRepo, firestoreRepo.GetClient())
	// Wire inline enrichment into import so eBay products are enriched synchronously during import
	importService.SetEnrichService(ebayEnrichService)
	log.Println("✅ eBay Browse Enrichment Service: Ready (inline enrichment enabled)")

	// Product AI Lookup (Create with AI)
	productAILookupHandler := handlers.NewProductAILookupHandler(productService, marketplaceRepo, marketplaceService, ebayEnrichService)
	log.Println("✅ Product AI Lookup Handler: Ready")

	// AI Consolidation

	log.Println("✅ AI Consolidation Service: Ready")
	// Initialize Search Handler (searchService already initialized above)
	searchHandler := handlers.NewSearchHandler(searchService, firestoreRepo, marketplaceRepo)

	var fileHandler *handlers.FileHandler
	if storageService != nil {
		fileHandler = handlers.NewFileHandler(storageService)
	}

	// ── MODULE K — AUTH, USERS, BILLING & USAGE ─────────────────────────────
	usageService := services.NewUsageService(firestoreRepo.GetClient())
	authHandler := handlers.NewAuthHandler(firestoreRepo.GetClient(), usageService)
	userHandler := handlers.NewUserHandler(firestoreRepo.GetClient())
	billingHandler := handlers.NewBillingHandler(firestoreRepo.GetClient(), usageService)

	// SESSION 5/6/7 — Security Settings, User Audit, App Store
	securitySettingsHandler := handlers.NewSecuritySettingsHandler(firestoreRepo.GetClient())
	userAuditHandler := handlers.NewUserAuditHandler(firestoreRepo.GetClient())
	appStoreHandler := handlers.NewAppStoreHandler(firestoreRepo.GetClient())
	log.Println("✅ Module K (Auth, Users, Billing): Ready")

	// ── MODULE L — PAGEBUILDER TEMPLATES ─────────────────────────────────────
	templateService := services.NewTemplateService(firestoreRepo.GetClient())
	templateHandler := handlers.NewTemplateHandlerWithClient(templateService, orderService, aiService, firestoreRepo.GetClient())
	// Wire templateService into handlers that fire automated email events
	orderManagementHandler.SetTemplateService(templateService)
	rmaHandler.SetTemplateService(templateService)
	dispatchHandler.SetTemplateService(templateService)
	orderService.SetTemplateService(templateService) // fires order_confirmation on all new downloaded orders
	log.Println("✅ Module L (Pagebuilder Templates): Ready")

	// Session 6.2 — Packing Slip Handler (requires templateService)
	packingSlipHandler := handlers.NewPackingSlipHandler(orderService, templateService)

	// ── SESSION 1 — CONFIGURATOR SYSTEM (CFG-01) ─────────────────────────────
	configuratorService := services.NewConfiguratorService(firestoreRepo.GetClient())
	configuratorHandler := handlers.NewConfiguratorHandler(configuratorService)
	log.Println("✅ Session 1 (Configurator System): Ready")

	// ── SESSION F — USP / DIFFERENTIATION ────────────────────────────────────
	configuratorAIHandler := handlers.NewConfiguratorAIHandler(aiService, configuratorService, firestoreRepo)
	listingAnalyticsHandler := handlers.NewListingAnalyticsHandler(marketplaceRepo, firestoreRepo, listingService)
	log.Println("✅ Session F (Configurator AI Setup, Listing Analytics): Ready")

	// ── MODULE G EXTENSION — AUTOMATION RULE ENGINE ───────────────────────
	automationEngine := services.NewRuleEngineWithTemplateService(
		firestoreRepo.GetClient(),
		automationSMTPConfig(),
		templateService,
	)
	cronScheduler := services.NewCronScheduler(automationEngine, orderService)
	automationHandler := handlers.NewAutomationHandlerWithScheduler(automationEngine, orderService, cronScheduler)

	// ── SESSION 7 — MACRO SCHEDULER ─────────────────────────────────────────
	// Runs every minute and fires scheduled macros (daily, weekly, monthly, etc.)
	executor := services.NewActionExecutorWithTemplateService(firestoreRepo.GetClient(), automationSMTPConfig(), templateService)
	macroScheduler := services.NewMacroScheduler(firestoreRepo.GetClient(), executor, templateService)
	go macroScheduler.Start(context.Background())
	log.Println("✅ Session 7: MacroScheduler started")
	// Load all tenants' scheduled rules and start the cron scheduler
	go func() {
		ctx := context.Background()
		// Iterate all tenant docs to load scheduled rules
		iter := firestoreRepo.GetClient().Collection("tenants").Documents(ctx)
		defer iter.Stop()
		for {
			doc, err := iter.Next()
			if err != nil {
				break
			}
			if loadErr := cronScheduler.LoadTenant(ctx, doc.Ref.ID); loadErr != nil {
				log.Printf("[cron] failed to load tenant %s: %v", doc.Ref.ID, loadErr)
			}
		}
		cronScheduler.Start()
		log.Println("✅ Cron scheduler started")
	}()
	log.Println("✅ Module G Extension (Automation Rule Engine): Ready")

	// ── SECTION D — ANALYTICS & REPORTING ───────────────────────────────────
	analyticsHandler := handlers.NewAnalyticsHandler(firestoreRepo.GetClient())
	reportHandler := handlers.NewReportHandler(firestoreRepo.GetClient())
	log.Println("✅ Section D (Analytics & Reports): Ready")

	// Temu Wizard Handler
	temuWizardHandler := handlers.NewTemuWizardHandler(firestoreRepo.GetClient(), marketplaceRepo, productService, aiService)
	if storageService != nil {
		temuWizardHandler.SetStorageService(storageService)
		log.Println("✅ Temu Wizard Handler: Ready (GCS XLSX storage enabled)")
	} else {
		log.Println("✅ Temu Wizard Handler: Ready (Firestore blob storage — GCS not configured)")
	}

	// Import Match Handler — second import matching flow
	importMatchHandler := handlers.NewImportMatchHandler(marketplaceRepo, firestoreRepo, searchService)
	log.Println("✅ Import Match Handler: Ready")

	// Wire match analyzer so pending_review jobs trigger analysis automatically
	importService.SetMatchAnalyzer(importMatchHandler)

	// ── KEYWORD INTELLIGENCE & SEO OPTIMISATION LAYER (Session 2) ────────────
	// DataForSEO client loads credentials from Secret Manager at startup.
	// Gracefully degraded — if credentials are absent the service falls back
	// to AI-generated keywords automatically.
	var dataForSEOClient *services.DataForSEOClient
	if dfsClient, dfsErr := services.NewDataForSEOClient(ctx); dfsErr != nil {
		log.Printf("⚠️  DataForSEO client unavailable (will use AI fallback): %v", dfsErr)
	} else {
		dataForSEOClient = dfsClient
		log.Println("✅ DataForSEO client: Ready")
	}
	// Amazon Ads client — loads client credentials from Secret Manager.
	// Returns nil if secrets are absent; enrichment degrades gracefully.
	var adsClient *services.AmazonAdsClient
	if ac, acErr := services.NewAmazonAdsClient(ctx); acErr != nil {
		log.Printf("⚠️  Amazon Ads client unavailable (credentials not configured): %v", acErr)
	} else if ac != nil {
		adsClient = ac
		log.Println("✅ Amazon Ads client: Ready")
	} else {
		log.Println("ℹ️  Amazon Ads client: secrets absent — Ads enrichment disabled until configured")
	}
	kwIntelSvc := services.NewKeywordIntelligenceService(firestoreRepo.GetClient(), dataForSEOClient, aiService, adsClient)
	kwScoreSvc := services.NewKeywordScoreService(firestoreRepo.GetClient())
	keywordIntelligenceHandler := handlers.NewKeywordIntelligenceHandler(
		firestoreRepo.GetClient(), kwIntelSvc, kwScoreSvc,
	)
	keywordIntelligenceHandler.SetUsageService(usageService)
	log.Println("✅ Keyword Intelligence & SEO Score Service: Ready")

	// ── SESSION 6 — KEYWORD CACHE SCHEDULER ──────────────────────────────────
	// Daily: refreshes stale global_keyword_cache entries (30-day TTL),
	//        propagates updated SEO scores to affected tenant listings.
	// Weekly: Brand Analytics SP-API pull for eligible tenants (stub until
	//         Brand Analytics role is approved).
	kwCacheScheduler := services.NewKeywordCacheScheduler(firestoreRepo.GetClient(), kwIntelSvc, kwScoreSvc)
	go kwCacheScheduler.Start(context.Background())
	log.Println("✅ Session 6: KeywordCacheScheduler started (daily refresh + weekly brand analytics)")

	// Wire keyword intelligence into the listing generation service so that
	// EnsureDataForSEOEnrichment is called before each generation run.
	aiListingGenSvc.SetKeywordIntelligenceService(kwIntelSvc)

	// Construct aiConsolidationHandler here so it can receive kwIntelSvc
	aiConsolidationHandler := handlers.NewAIConsolidationHandler(
		aiConsolidationSvc, aiListingGenSvc, marketplaceRepo, productService, firestoreRepo.GetClient(), searchService, kwIntelSvc,
	)
	log.Println("✅ AI Consolidation Handler: wired with keyword intelligence")

	// ── SESSION 8: Wire keyword context into single-listing generation paths ──
	// aiHandler.SetKeywordService and SetListingGenService inject the same
	// keyword intelligence available in aiConsolidationHandler into GenerateSingle
	// and GenerateWithSchema, so all AI generation paths use keyword context.
	aiHandler.SetKeywordService(kwIntelSvc)
	aiHandler.SetListingGenService(aiListingGenSvc)
	log.Println("✅ Session 8: AI single-listing handler wired with keyword intelligence")

	// ── SESSION 9: Wire credit gating into AI handler ─────────────────────────
	// GenerateSingle and GenerateWithSchema now check and deduct credits before
	// any AI call. usageService and firestoreClient are already in scope from
	// earlier in main(). Pattern matches keywordIntelligenceHandler.SetUsageService
	// already called above at line ~839.
	aiHandler.SetUsageService(usageService)
	aiHandler.SetFirestoreClient(firestoreRepo.GetClient())
	log.Println("✅ Session 9: AI handler credit gating wired")

	// ── SESSION 7: BULK OPTIMISE & ADMIN COST ─────────────────────────────────
	// Constructed here (not inside setupRouter) because they need firestoreRepo
	// and usageService which are only in scope in main().
	bulkOptimiseHandler := handlers.NewBulkOptimiseHandler(firestoreRepo.GetClient(), usageService)
	adminHandler := handlers.NewAdminHandler(firestoreRepo.GetClient())

	// Setup router
	router := setupRouter(
		productHandler,
		fileHandler,
		attributeHandler,
		marketplaceHandler,
		temuHandler,
		ebayHandler,
		amazonHandler,
		amazonOAuthHandler,
		amazonSchemaHandler,
		ebaySchemaHandler,
		temuSchemaHandler,
		tiktokHandler,
		tiktokOrdersHandler,
		etsyHandler,
		etsyOrdersHandler,
		wooHandler,
		wooOrdersHandler,
		walmartHandler,
		walmartOrdersHandler,
		kauflandHandler,
		kauflandOrdersHandler,
		magentoHandler,
		magentoOrdersHandler,
		bigcommerceHandler,
		bigcommerceOrdersHandler,
		onbuyHandler,
		onbuyOrdersHandler,
		blueparkHandler,
		wishHandler,
		extractHandler,
		exportHandler,
		importHandler,
		tenantHandler,
		searchHandler,
		aiHandler,
		orderHandler,
		orchestratorHandler,
		amazonOrdersHandler,
		ebayOrdersHandler,
		temuOrdersHandler,
		orderWebhookHandler,
		orderSyncHandler,
		miraklHandler,
		miraklOrdersHandler,
		orderActionsHandler,
		orderActionsExtendedHandler,
		inventoryHandler,
		warehouseLocationHandler,
		dispatchHandler,
		workflowHandler,
		fulfilmentSourceHandler,
		supplierHandler,
		purchaseOrderHandler,
		authHandler,
		userHandler,
		billingHandler,
		templateHandler,
		automationHandler,
		rmaHandler,
		refundDownloadHandler,
		refundPushHandler,
		messagingHandler,
		settingsHandler,
		ebayEnrichHandler,
		opsHandler,
		aiConsolidationHandler,
		stockCountHandler,
		stockScrapHandler,
		productAILookupHandler,
		forecastingHandler,
		// Gap Closure handlers
		batchHandler,
		fbaInboundHandler,
		syncStatusHandler,
		inventorySyncHandler,
		stockReservationHandler,
		binrackHandler,
		postageDefinitionHandler,
		vendorOrderHandler,
		productExtensionsHandler,
		supplierReturnHandler,
		picklistHandler,
		labelPrintingHandler,
		// P0 handlers
		listingDescriptionHandler,
		inventoryViewHandler,
		automationLogHandler,
		storageGroupHandler,
		priceSyncHandler,
		productExtHandler,
		// B-series order management handlers
		orderViewHandler,
		orderManagementHandler,
		// H-001 changelog
		changelogHandler,
		// D-001/D-002 analytics & reports
		analyticsHandler,
		reportHandler,
		// Session 1 — Configurator System
		configuratorHandler,
		// Session 5 — Shopify Listing Handler (PRC-01)
		shopifyListingHandler,
		shopifyHandler,
		// Shopline
		shoplineListingHandler,
		shoplineHandler,
		// Session 6 — SKU Check (FLD-15)
		skuCheckHandler,
		// Session F — USP / Differentiation
		configuratorAIHandler,
		listingAnalyticsHandler,
		// Session 4 — Back Market, Zalando, Bol.com, Lazada
		backmarketOrdersHandler,
		zalandoOrdersHandler,
		bolOrdersHandler,
		lazadaOrdersHandler,
		reconcileHandler,
		// Session 18/19 — ShopWired
		shopwiredHandler,
		shopwiredOrdersHandler,
		// Session 6.2 — S4 Bulk Order Handlers + Packing Slip
		backmarketBulkHandler,
		zalandoBulkHandler,
		bolBulkHandler,
		lazadaBulkHandler,
		packingSlipHandler,
		// Session 9 — Auto Reorder
		autoReorderHandler,
		// Session 1 — Navigation & Global UI
		notificationHandler,
		emailTemplateHandler,
		emailLogHandler,
		sentMailHandler,
		pickwaveHandler,
		reorderSuggestionHandler,
		// Session 3 — Tracking Webhooks + Returns Portal
		trackingWebhookHandler,
		returnsPortalHandler,
		// Session 3 — Dispatch Extensions
		dispatchExtHandler,
		// Evri Tracking Sync + Shipping Templates + Customs Profiles
		trackingSyncHandler,
		shippingTemplateHandler,
		customsProfileHandler,
		// Session 5/6/7 — Security Settings, User Audit, App Store
		securitySettingsHandler,
		userAuditHandler,
		appStoreHandler,
		// New handlers from PROMPT_05
		binTypeHandler,
		scheduleHandler,
		temuWizardHandler,
		importMatchHandler,
		// Keyword Intelligence & SEO (Session 1)
		keywordIntelligenceHandler,
		amazonMessagingWebhookHandler,
		messagingAIHandler,
		cancellationAlertHandler,
		systemHandler,
		func() gin.HandlerFunc {
			if os.Getenv("DISABLE_AUTH") == "true" {
				return middleware.TenantMiddleware()
			}
			return middleware.AuthMiddleware(firestoreRepo.GetClient())
		}(),
		bulkOptimiseHandler,
		adminHandler,
		pimHandler,
	)

	// ── A-007: Low-stock notification service ────────────────────────────────
	// Runs every 6 hours in background; checks all tenants for items at or
	// below reorder point and fires in-app notifications (24h cooldown per SKU).
	stockAlertSvc := services.NewStockAlertService(firestoreRepo.GetClient())
	go func() {
		// Initial run after 2 minutes (allow warm-up)
		time.Sleep(2 * time.Minute)
		ctx := context.Background()
		stockAlertSvc.CheckAllTenants(ctx)
		// Then every 6 hours
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			stockAlertSvc.CheckAllTenants(ctx)
		}
	}()
	log.Println("✅ Stock Alert Service: Scheduled (every 6h)")

	// ── Session 3 Task 4: Reorder Suggestion Generator ───────────────────────
	// Piggybacks on the same 6h schedule as stock alerts, generating pending
	// PO suggestions for low-stock items that don't already have one.
	go func() {
		time.Sleep(3 * time.Minute) // slight offset from stock alert run
		ctx := context.Background()
		// Iterate all tenants
		iter := firestoreRepo.GetClient().Collection("tenants").Documents(ctx)
		for {
			doc, err := iter.Next()
			if err != nil {
				break
			}
			reorderSuggestionHandler.GenerateForTenantBackground(doc.Ref.ID)
		}
		iter.Stop()
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			ctx2 := context.Background()
			iter2 := firestoreRepo.GetClient().Collection("tenants").Documents(ctx2)
			for {
				doc, err := iter2.Next()
				if err != nil {
					break
				}
				reorderSuggestionHandler.GenerateForTenantBackground(doc.Ref.ID)
			}
			iter2.Stop()
		}
	}()
	log.Println("✅ Reorder Suggestion Generator: Scheduled (every 6h)")

	// ── ENH-02: Amazon schema auto-refresh scheduler ─────────────────────────
	// Checks every hour; triggers a schema download job for tenants where
	// auto-refresh is enabled and the configured interval has elapsed.
	schemaRefreshScheduler := handlers.NewSchemaRefreshScheduler(firestoreRepo.GetClient(), amazonSchemaHandler)
	amazonSchemaHandler.SetScheduler(schemaRefreshScheduler)
	schemaRefreshScheduler.Run()
	log.Println("✅ Schema Auto-Refresh Scheduler: Running (checks every 1h)")

	// ── USP-04: eBay schema auto-refresh scheduler ────────────────────────────
	ebaySchemaRefreshScheduler := handlers.NewEbaySchemaRefreshScheduler(firestoreRepo.GetClient(), ebaySchemaHandler)
	ebaySchemaHandler.SetScheduler(ebaySchemaRefreshScheduler)
	ebaySchemaRefreshScheduler.Run()
	log.Println("✅ eBay Schema Auto-Refresh Scheduler: Running (checks every 1h)")

	// ── USP-04: Temu schema auto-refresh scheduler ────────────────────────────
	temuSchemaRefreshScheduler := handlers.NewTemuSchemaRefreshScheduler(firestoreRepo.GetClient(), temuSchemaHandler)
	temuSchemaHandler.SetScheduler(temuSchemaRefreshScheduler)
	temuSchemaRefreshScheduler.Run()
	log.Println("✅ Temu Schema Auto-Refresh Scheduler: Running (checks every 1h)")

	// ── Session 2 Task 3: Inventory Sync Scheduler ───────────────────────────
	// Runs every 15 minutes; pushes stock levels to all channels with
	// inventory_sync_enabled=true, applying min/max/buffer rules from config.
	inventorySyncScheduler := handlers.NewInventorySyncScheduler(firestoreRepo.GetClient(), inventorySyncHandler)
	inventorySyncScheduler.Run()
	log.Println("✅ Inventory Sync Scheduler: Running (every 15 minutes)")

	// ── Messaging Sync Scheduler ─────────────────────────────────────────────
	// Pulls new buyer messages from Amazon and eBay every 30 minutes for all
	// tenants. Runs in background goroutines (one per credential) so a slow
	// channel never blocks others. Initial run after 5 minutes to allow
	// warm-up and avoid hammering channels on every deploy restart.
	go func() {
		time.Sleep(5 * time.Minute)
		ctx := context.Background()
		log.Println("[Messaging] Running initial message sync...")
		messagingHandler.SyncAllTenants(ctx)
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			log.Println("[Messaging] Running scheduled message sync...")
			messagingHandler.SyncAllTenants(context.Background())
		}
	}()
	log.Println("✅ Messaging Sync Scheduler: Running (every 30 minutes)")

	// ── Amazon Messaging Webhook Registration ────────────────────────────────
	// Registers MESSAGING_NEW_MESSAGE_NOTIFICATION webhooks for all existing
	// Amazon credentials that don't have one yet. New credentials are
	// registered when they're saved. The 30-min scheduler acts as a fallback.
	go func() {
		time.Sleep(2 * time.Minute) // allow warm-up before hitting SP-API
		amazonMessagingWebhookHandler.RegisterAllExistingCredentials(
			context.Background(),
			os.Getenv("BACKEND_URL"),
		)
	}()
	log.Println("✅ Amazon Messaging Webhook: Registration queued (runs after 2m warm-up)")

	// ── Webhook Health Check Scheduler ──────────────────────────────────────
	// Runs at startup (after 3 min warm-up) then every 6 hours.
	// Scans all active credentials and re-registers any missing webhooks.
	// Results written to tenants/{tid}/webhook_health for UI visibility.
	go func() {
		time.Sleep(3 * time.Minute)
		bkURL := os.Getenv("BACKEND_URL")
		log.Println("[WebhookHealth] Running startup health check...")
		orderWebhookHandler.CheckAllWebhookSubscriptions(
			context.Background(),
			bkURL,
			amazonMessagingWebhookHandler,
		)
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			log.Println("[WebhookHealth] Running scheduled health check...")
			orderWebhookHandler.CheckAllWebhookSubscriptions(
				context.Background(),
				bkURL,
				amazonMessagingWebhookHandler,
			)
		}
	}()
	log.Println("✅ Webhook Health Checker: Scheduled (startup + every 6h)")

	// ── Credential Audit Scheduler ───────────────────────────────────────────
	// Tests every credential across all tenants against the live API.
	// Marks failed credentials inactive and reactivates recovered ones.
	// Runs at startup (4 min delay) then every 6 hours.
	go func() {
		time.Sleep(4 * time.Minute)
		log.Println("[CredentialAudit] Running startup audit...")
		marketplaceHandler.RunCredentialAuditBackground(context.Background())
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			log.Println("[CredentialAudit] Running scheduled audit...")
			marketplaceHandler.RunCredentialAuditBackground(context.Background())
		}
	}()
	log.Println("✅ Credential Audit: Scheduled (startup + every 6h)")

	// Subscribe all active Temu credentials to webhook events (Step 4).
	// Runs 5 minutes after startup to ensure credentials are fully loaded,
	// then daily to pick up any new credentials added since last run.
	go func() {
		time.Sleep(5 * time.Minute)
		log.Println("[TemuWebhookSub] Running startup subscription...")
		results := temuHandler.SubscribeAllTemuCredentials(context.Background())
		ok := 0
		for _, r := range results {
			if r.Success {
				ok++
			}
		}
		log.Printf("[TemuWebhookSub] Startup complete: %d/%d credentials subscribed", ok, len(results))
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			log.Println("[TemuWebhookSub] Running daily subscription refresh...")
			temuHandler.SubscribeAllTemuCredentials(context.Background())
		}
	}()
	log.Println("✅ Temu Webhook Subscription: Scheduled (startup + daily)")

	// Start server
	log.Printf("🚀 MarketMate Platform starting on port %s", port)
	log.Printf("📦 Module A (PIM): Ready")
	log.Printf("🌐 Module B (Marketplace): Ready")
	log.Printf("🛍️  Temu Integration: Ready")
	log.Printf("🏷️  eBay Integration: Ready")
	log.Printf("📦 Amazon Integration: Ready")
	log.Printf("🛒 Order Management: Ready")
	log.Printf("🛒 Tesco Integration: Ready")
	log.Printf("📦 GCS Bucket: %s", gcsBucket)
	log.Printf("🤖 AI Listing Generation: Ready")

	// ── Dev Tools (registered after setupRouter — uses variables in main() scope) ─
	orderSeedHandler := handlers.NewOrderSeedHandler(orderService, firestoreRepo)
	devAuth := func() gin.HandlerFunc {
		if os.Getenv("DISABLE_AUTH") == "true" {
			return middleware.TenantMiddleware()
		}
		return middleware.AuthMiddleware(firestoreRepo.GetClient())
	}()
	router.POST("/api/v1/dev/orders/seed", devAuth, orderSeedHandler.SeedOrders)

	if err := router.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func automationSMTPConfig() *services.SMTPConfig {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		return nil
	}
	return &services.SMTPConfig{
		Host:     host,
		Port:     os.Getenv("SMTP_PORT"),
		User:     os.Getenv("SMTP_USERNAME"), // matches SMTP_USERNAME in .env
		Password: os.Getenv("SMTP_PASSWORD"),
		From:     os.Getenv("SMTP_FROM"),
	}
}

func getAIModeStr(svc *services.AIService) string {
	if svc.HasGemini() && svc.HasClaude() {
		return "hybrid (Gemini+Claude)"
	}
	if svc.HasClaude() {
		return "claude-only"
	}
	if svc.HasGemini() {
		return "gemini-only"
	}
	return "unavailable"
}

// ============================================================================
// SEED GLOBAL MARKETPLACE KEYS
// ============================================================================

func seedGlobalMarketplaceKeys(ctx context.Context, repo *repository.GlobalConfigRepository) {
	// ── Amazon SP-API keys (OAuth app) ──
	// Uses LWA OAuth only — no AWS signing credentials needed.
	amazonKeys := map[string]string{}
	amazonEnvMappings := map[string]string{
		"AMAZON_LWA_CLIENT_ID":     "lwa_client_id",
		"AMAZON_LWA_CLIENT_SECRET": "lwa_client_secret",
		"AMAZON_APP_ID":            "app_id",
		"AMAZON_REDIRECT_URI":      "redirect_uri",
	}
	hasAmazon := false
	for envKey, credKey := range amazonEnvMappings {
		val := os.Getenv(envKey)
		if val != "" {
			amazonKeys[credKey] = val
			hasAmazon = true
		}
	}
	if hasAmazon {
		if err := repo.SaveMarketplaceKeys(ctx, "amazon", amazonKeys); err != nil {
			log.Printf("⚠️  Failed to seed Amazon global keys: %v", err)
		} else {
			log.Printf("✅ Amazon global keys seeded (%d keys)", len(amazonKeys))
		}
	}

	// ── Temu API keys (global — shared across all tenants) ──
	temuKeys := map[string]string{}
	temuEnvMappings := map[string]string{
		"TEMU_UK_APP_KEY":    "app_key",
		"TEMU_UK_APP_SECRET": "app_secret",
		"TEMU_UK_BASE_URL":   "base_url",
		"TEMU_APP_KEY":       "app_key",
		"TEMU_APP_SECRET":    "app_secret",
		"TEMU_ACCESS_TOKEN":  "access_token",
		"TEMU_BASE_URL":      "base_url",
	}
	hasTemuKeys := false
	for envKey, credKey := range temuEnvMappings {
		val := os.Getenv(envKey)
		if val != "" {
			if _, exists := temuKeys[credKey]; !exists || val != "" {
				temuKeys[credKey] = val
				hasTemuKeys = true
			}
		}
	}
	if _, ok := temuKeys["base_url"]; !ok {
		temuKeys["base_url"] = "https://openapi-b-eu.temu.com/openapi/router"
	}
	if hasTemuKeys {
		if err := repo.SaveMarketplaceKeys(ctx, "temu", temuKeys); err != nil {
			log.Printf("⚠️  Failed to seed Temu global keys: %v", err)
		} else {
			log.Printf("✅ Temu global keys seeded (%d keys)", len(temuKeys))
		}
	}

	seedListingFieldBlocks(ctx, repo)

	// ── eBay API keys ──
	ebayKeys := map[string]string{}
	ebayEnvMappings := map[string]string{
		"EBAY_PROD_CLIENT_ID":     "client_id",
		"EBAY_PROD_CLIENT_SECRET": "client_secret",
		"EBAY_DEV_ID":             "dev_id",
		"EBAY_RUNAME":             "ru_name",
		"EBAY_PROD_OAUTH_TOKEN":   "oauth_token",
	}
	hasEbayKeys := false
	for envKey, credKey := range ebayEnvMappings {
		val := os.Getenv(envKey)
		if val != "" {
			ebayKeys[credKey] = val
			hasEbayKeys = true
		}
	}
	if hasEbayKeys {
		if err := repo.SaveMarketplaceKeys(ctx, "ebay", ebayKeys); err != nil {
			log.Printf("⚠️  Failed to seed eBay global keys: %v", err)
		} else {
			log.Printf("✅ eBay global keys seeded (%d keys)", len(ebayKeys))
		}
	}
}

func seedListingFieldBlocks(ctx context.Context, repo *repository.GlobalConfigRepository) {
	defaultBlocked := []string{
		"product_site_launch_date",
		"recommended_browse_nodes",
		"supplier_declared_dg_hz_regulation",
		"gpsr_safety_attestation",
		"unspsc_code",
		"externally_assigned_product_identifier",
	}

	if err := repo.SeedIfNotExists(ctx, "listing_field_blocks", map[string]interface{}{
		"blocked_fields": defaultBlocked,
		"description":    "Fields excluded from listing enrichment.",
		"updated_at":     time.Now(),
	}); err != nil {
		log.Printf("⚠️  Failed to seed listing field blocks: %v", err)
	} else {
		log.Printf("✅ Listing field blocks config ready")
	}
}

func setupRouter(
	productHandler *handlers.ProductHandler,
	fileHandler *handlers.FileHandler,
	attributeHandler *handlers.AttributeHandler,
	marketplaceHandler *handlers.MarketplaceHandler,
	temuHandler *handlers.TemuHandler,
	ebayHandler *handlers.EbayHandler,
	amazonHandler *handlers.AmazonHandler,
	amazonOAuthHandler *handlers.AmazonOAuthHandler,
	amazonSchemaHandler *handlers.AmazonSchemaHandler,
	ebaySchemaHandler *handlers.EbaySchemaHandler,
	temuSchemaHandler *handlers.TemuSchemaHandler,
	tiktokHandler *handlers.TikTokHandler,
	tiktokOrdersHandler *handlers.TikTokOrdersHandler,
	etsyHandler *handlers.EtsyHandler,
	etsyOrdersHandler *handlers.EtsyOrdersHandler,
	wooHandler *handlers.WooCommerceHandler,
	wooOrdersHandler *handlers.WooCommerceOrdersHandler,
	walmartHandler *handlers.WalmartHandler,
	walmartOrdersHandler *handlers.WalmartOrdersHandler,
	kauflandHandler *handlers.KauflandHandler,
	kauflandOrdersHandler *handlers.KauflandOrdersHandler,
	magentoHandler *handlers.MagentoHandler,
	magentoOrdersHandler *handlers.MagentoOrdersHandler,
	bigcommerceHandler *handlers.BigCommerceHandler,
	bigcommerceOrdersHandler *handlers.BigCommerceOrdersHandler,
	onbuyHandler *handlers.OnBuyHandler,
	onbuyOrdersHandler *handlers.OnBuyOrdersHandler,
	blueparkHandler *handlers.BlueparkHandler,
	wishHandler *handlers.WishHandler,
	extractHandler *handlers.ExtractHandler,
	exportHandler *handlers.ExportHandler,
	importHandler *handlers.ImportHandler,
	tenantHandler *handlers.TenantHandler,
	searchHandler *handlers.SearchHandler,
	aiHandler *handlers.AIHandler,
	orderHandler *handlers.OrderHandler,
	orchestratorHandler *handlers.OrchestratorHandler,
	amazonOrdersHandler *handlers.AmazonOrdersHandler,
	ebayOrdersHandler *handlers.EbayOrdersHandler,
	temuOrdersHandler *handlers.TemuOrdersHandler,
	orderWebhookHandler *handlers.OrderWebhookHandler,
	orderSyncHandler *handlers.OrderSyncHandler,
	miraklHandler *handlers.MiraklHandler,
	miraklOrdersHandler *handlers.MiraklOrdersHandler,
	orderActionsHandler *handlers.OrderActionsHandler,
	orderActionsExtendedHandler *handlers.OrderActionsExtendedHandler,
	inventoryHandler *handlers.InventoryHandler,
	warehouseLocationHandler *handlers.WarehouseLocationHandler,
	dispatchHandler *handlers.DispatchHandler,
	workflowHandler *handlers.WorkflowHandler,
	fulfilmentSourceHandler *handlers.FulfilmentSourceHandler,
	supplierHandler *handlers.SupplierHandler,
	purchaseOrderHandler *handlers.PurchaseOrderHandler,
	authHandler *handlers.AuthHandler,
	userHandler *handlers.UserHandler,
	billingHandler *handlers.BillingHandler,
	templateHandler *handlers.TemplateHandler,
	automationHandler *handlers.AutomationHandler,
	rmaHandler *handlers.RMAHandler,
	refundDownloadHandler *handlers.RefundDownloadHandler,
	refundPushHandler *handlers.RefundPushHandler,
	messagingHandler *handlers.MessagingHandler,
	settingsHandler *handlers.SettingsHandler,
	ebayEnrichHandler *handlers.EbayEnrichmentHandler,
	opsHandler *handlers.OpsHandler,
	aiConsolidationHandler *handlers.AIConsolidationHandler,
	stockCountHandler *handlers.StockCountHandler,
	stockScrapHandler *handlers.StockScrapHandler,
	productAILookupHandler *handlers.ProductAILookupHandler,
	forecastingHandler *handlers.ForecastingHandler,
	// Gap Closure handlers
	batchHandler *handlers.BatchHandler,
	fbaInboundHandler *handlers.FBAInboundHandler,
	syncStatusHandler *handlers.SyncStatusHandler,
	inventorySyncHandler *handlers.InventorySyncHandler,
	stockReservationHandler *handlers.StockReservationHandler,
	binrackHandler *handlers.BinrackHandler,
	postageDefinitionHandler *handlers.PostageDefinitionHandler,
	vendorOrderHandler *handlers.VendorOrderHandler,
	productExtensionsHandler *handlers.ProductExtensionsHandler,
	supplierReturnHandler *handlers.SupplierReturnHandler,
	picklistHandler *handlers.PicklistHandler,
	labelPrintingHandler *handlers.LabelPrintingHandler,
	// P0 handlers
	listingDescriptionHandler *handlers.ListingDescriptionHandler,
	inventoryViewHandler *handlers.InventoryViewHandler,
	automationLogHandler *handlers.AutomationLogHandler,
	storageGroupHandler *handlers.StorageGroupHandler,
	priceSyncHandler *handlers.PriceSyncHandler,
	productExtHandler *handlers.ProductExtHandler,
	// B-series order management
	orderViewHandler *handlers.OrderViewHandler,
	orderManagementHandler *handlers.OrderManagementHandler,
	// H-001 changelog
	changelogHandler *handlers.ChangelogHandler,
	// D-001/D-002 analytics & reports
	analyticsHandler *handlers.AnalyticsHandler,
	reportHandler *handlers.ReportHandler,
	// Session 1 — Configurator System
	configuratorHandler *handlers.ConfiguratorHandler,
	// Session 5 — Shopify Listing Handler (PRC-01)
	shopifyListingHandler *handlers.ShopifyListingHandler,
	shopifyHandler *handlers.ShopifyHandler,
	// Shopline
	shoplineListingHandler *handlers.ShoplineListingHandler,
	shoplineHandler *handlers.ShoplineHandler,
	// Session 6 — SKU Check (FLD-15)
	skuCheckHandler *handlers.SKUCheckHandler,
	// Session F — USP / Differentiation
	configuratorAIHandler *handlers.ConfiguratorAIHandler,
	listingAnalyticsHandler *handlers.ListingAnalyticsHandler,
	// Session 4 — Back Market, Zalando, Bol.com, Lazada
	backmarketOrdersHandler *handlers.BackMarketOrdersHandler,
	zalandoOrdersHandler *handlers.ZalandoOrdersHandler,
	bolOrdersHandler *handlers.BolOrdersHandler,
	lazadaOrdersHandler *handlers.LazadaOrdersHandler,
	reconcileHandler *handlers.ReconcileHandler,
	// Session 18/19 — ShopWired
	shopwiredHandler *handlers.ShopWiredHandler,
	shopwiredOrdersHandler *handlers.ShopWiredOrdersHandler,
	// Session 6.2 — S4 Bulk Order Handlers + Packing Slip
	backmarketBulkHandler *handlers.BackMarketBulkHandler,
	zalandoBulkHandler *handlers.ZalandoBulkHandler,
	bolBulkHandler *handlers.BolBulkHandler,
	lazadaBulkHandler *handlers.LazadaBulkHandler,
	packingSlipHandler *handlers.PackingSlipHandler,
	// Session 9
	autoReorderHandler *handlers.AutoReorderHandler,
	// Session 1 — Navigation & Global UI
	notificationHandler *handlers.NotificationHandler,
	emailTemplateHandler *handlers.EmailTemplateHandler,
	emailLogHandler *handlers.EmailLogHandler,
	sentMailHandler *handlers.SentMailHandler,
	pickwaveHandler *handlers.PickwaveHandler,
	reorderSuggestionHandler *handlers.ReorderSuggestionHandler,
	// Session 3 — Tracking Webhooks + Returns Portal
	trackingWebhookHandler *handlers.TrackingWebhookHandler,
	returnsPortalHandler *handlers.ReturnsPortalHandler,
	// Session 3 — Dispatch Extensions
	dispatchExtHandler *handlers.DispatchExtensionsHandler,
	// Evri tracking sync + shipping templates + customs profiles
	trackingSyncHandler     *handlers.TrackingSyncHandler,
	shippingTemplateHandler *handlers.ShippingTemplateHandler,
	customsProfileHandler   *handlers.CustomsProfileHandler,
	// Session 5/6/7 — Security Settings, User Audit, App Store
	securitySettingsHandler *handlers.SecuritySettingsHandler,
	userAuditHandler *handlers.UserAuditHandler,
	appStoreHandler *handlers.AppStoreHandler,
	// New handlers from PROMPT_05 sessions
	binTypeHandler *handlers.BinTypeHandler,
	scheduleHandler *handlers.ScheduleHandler,
	temuWizardHandler *handlers.TemuWizardHandler,
	importMatchHandler *handlers.ImportMatchHandler,
	// Keyword Intelligence & SEO (Session 1)
	keywordIntelligenceHandler *handlers.KeywordIntelligenceHandler,
	amazonMessagingWebhookHandler *handlers.AmazonMessagingWebhookHandler,
	messagingAIHandler *handlers.MessagingAIHandler,
	cancellationAlertHandler *handlers.CancellationAlertHandler,
	systemHandler *handlers.SystemHandler,
	tenantAuth gin.HandlerFunc,
	// Session 7 — passed from main() to avoid scope issues
	bulkOptimiseHandler *handlers.BulkOptimiseHandler,
	adminHandler *handlers.AdminHandler,
	// PIM Import/Export handler
	pimHandler *handlers.PIMImportHandler,
) *gin.Engine {
	router := gin.Default()

	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://localhost:5174", "http://localhost:3000", "https://marketmate-486116.web.app", "https://marketmate-486116.firebaseapp.com"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Tenant-Id"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	// Security headers middleware
	// Adds HTTP security headers to all responses — fixes ZAP DAST findings
	// and satisfies Amazon SP-API PCD security requirements.
	router.Use(func(c *gin.Context) {
		// HSTS — instructs browsers to always use HTTPS for this domain
		// max-age=31536000 = 1 year; includeSubDomains covers all subdomains
		c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		// Prevent MIME type sniffing attacks
		c.Header("X-Content-Type-Options", "nosniff")
		// Prevent clickjacking from external sites.
		// SAMEORIGIN (not DENY) allows Firebase Hosting to render the app correctly
		// while still blocking framing from any external domain.
		c.Header("X-Frame-Options", "SAMEORIGIN")
		// XSS protection for older browsers that don't support CSP
		c.Header("X-XSS-Protection", "1; mode=block")
		// Control referrer information sent with requests
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		// Restrict access to browser features not used by this API
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Next()
	})

	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})

	router.GET("/api/v1/system/memory", systemHandler.GetMemory)

	router.GET("/api/v1/ebay/oauth/callback", ebayHandler.OAuthCallback)

	router.GET("/api/v1/tenants", tenantHandler.ListTenants)
	router.POST("/api/v1/tenants", tenantHandler.CreateTenant)
	router.DELETE("/api/v1/tenants/:id", tenantHandler.DeleteTenant)

	api := router.Group("/api/v1")
	api.Use(tenantAuth)

	api.GET("/status", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "running",
			"modules": []string{"Module A - PIM", "Module B - Marketplace", "Module D - Inventory", "Module E - Orders", "Module F - Dispatch"},
			"version": "1.0.0",
		})
	})

	// MODULE A - PRODUCT MANAGEMENT
	api.POST("/products/ai-lookup", productAILookupHandler.Lookup)
	api.GET("/products", productHandler.ListProducts)
	api.POST("/products", productHandler.CreateProduct)
	api.GET("/products/:id", productHandler.GetProduct)
	api.PATCH("/products/:id", productHandler.UpdateProduct)
	api.DELETE("/products/:id", productHandler.DeleteProduct)
	api.POST("/products/:id/duplicate", productHandler.DuplicateProduct)
	api.GET("/products/:id/variants", productHandler.ListVariants)
	api.POST("/products/:id/variants", productHandler.CreateVariant)
	api.POST("/products/:id/generate-variants", productHandler.GenerateVariants)
	api.GET("/variants", productHandler.ListAllVariants)
	api.GET("/variants/:id", productHandler.GetVariant)
	api.PATCH("/variants/:id", productHandler.UpdateVariant)
	api.DELETE("/variants/:id", productHandler.DeleteVariant)
	api.GET("/categories", productHandler.ListCategories)
	api.GET("/categories/tree", productHandler.GetCategoryTree)
	api.POST("/categories", productHandler.CreateCategory)
	api.GET("/categories/:id", productHandler.GetCategory)
	api.PATCH("/categories/:id", productHandler.UpdateCategory)
	api.DELETE("/categories/:id", productHandler.DeleteCategory)
	api.GET("/attributes", attributeHandler.ListAttributes)
	api.POST("/attributes", attributeHandler.CreateAttribute)
	api.GET("/attributes/:id", attributeHandler.GetAttribute)
	api.PATCH("/attributes/:id", attributeHandler.UpdateAttribute)
	api.DELETE("/attributes/:id", attributeHandler.DeleteAttribute)
	api.GET("/attribute-sets", attributeHandler.ListAttributeSets)
	api.POST("/attribute-sets", attributeHandler.CreateAttributeSet)
	api.GET("/attribute-sets/:id", attributeHandler.GetAttributeSet)
	api.PATCH("/attribute-sets/:id", attributeHandler.UpdateAttributeSet)
	api.DELETE("/attribute-sets/:id", attributeHandler.DeleteAttributeSet)

	if fileHandler != nil {
		api.POST("/upload", fileHandler.UploadFile)
		api.DELETE("/files", fileHandler.DeleteFile)
		api.GET("/files/list", fileHandler.ListFiles)
	}

	api.POST("/products/bulk", productHandler.BulkCreateProducts)
	api.PATCH("/products/bulk", productHandler.BulkUpdateProducts)
	api.GET("/products/:id/due-stock", productExtHandler.GetDueStock)

	// EXPORT / IMPORT
	api.GET("/products/export", exportHandler.ExportProducts)
	api.GET("/products/export/stream", exportHandler.StreamProductsCSV)
	api.POST("/export/queue", exportHandler.QueueExport)
	api.GET("/export/jobs", exportHandler.ListExportJobs)
	api.GET("/orders/export", exportHandler.ExportOrders)
	api.GET("/products/export/prices", exportHandler.ExportPrices)
	api.GET("/products/export/stock", exportHandler.ExportStock)
	api.GET("/products/export/template", exportHandler.ExportTemplate)
	api.POST("/products/import/dry-run", exportHandler.ImportDryRun)
	api.POST("/products/import", exportHandler.ImportProducts)
	api.POST("/products/import/stock/dry-run", exportHandler.ImportStockDryRun)
	api.POST("/products/import/stock", exportHandler.ImportStock)
	api.POST("/products/import/prices/dry-run", exportHandler.ImportPricesDryRun)
	api.POST("/products/import/prices", exportHandler.ImportPrices)

	// UNIFIED IMPORT/EXPORT (new CSV/XLSX hub)
	api.POST("/export", exportHandler.UnifiedExport)
	api.GET("/export/rma", exportHandler.ExportRMAs)
	api.GET("/export/purchase-orders", exportHandler.ExportPurchaseOrders)
	api.GET("/export/shipments", exportHandler.ExportShipments)
	api.POST("/import/validate", importHandler.ValidateImport)
	api.POST("/import/preview", importHandler.PreviewImport)
	api.POST("/import/apply", importHandler.ApplyImport)
	api.GET("/import/status/:job_id", importHandler.GetImportStatus)
	api.GET("/import/history", importHandler.GetImportHistory)
	api.GET("/import/templates/:type", importHandler.GetTemplate)
	api.DELETE("/import/jobs/:id", importHandler.DeleteImportJob)

	// PIM IMPORT/EXPORT — /import-export nav item (CSV/XLSX bulk product catalogue management)
	// Completely separate from /marketplace/import — no overlap with channel import flows.
	api.POST("/pim/import/preview", pimHandler.PreviewImport)
	api.POST("/pim/import/validate", pimHandler.ValidateImport)
	api.POST("/pim/import/apply", pimHandler.ApplyImport)
	api.GET("/pim/import/status/:id", pimHandler.GetImportStatus)
	api.DELETE("/pim/import/jobs/:id", pimHandler.DeleteImportJob)
	api.GET("/pim/import/history", pimHandler.GetImportHistory)
	api.GET("/pim/template", pimHandler.GetTemplate)
	api.GET("/pim/export", pimHandler.ExportPIMProducts)
	api.POST("/pim/export", pimHandler.ExportPIMProducts)

	api.GET("/jobs/:id", productHandler.GetJob)
	api.GET("/jobs", productHandler.ListJobs)

	// MODULE B - MARKETPLACE
	api.POST("/marketplace/credentials", marketplaceHandler.CreateCredential)
	api.GET("/marketplace/credentials", marketplaceHandler.ListCredentials)
	api.GET("/marketplace/credentials/:id", marketplaceHandler.GetCredential)
	api.DELETE("/marketplace/credentials/:id", marketplaceHandler.DeleteCredential)
	api.PATCH("/marketplace/credentials/:id", marketplaceHandler.PatchCredential)
	api.POST("/marketplace/credentials/:id/test", marketplaceHandler.TestConnection)
	api.POST("/marketplace/credentials/audit", marketplaceHandler.AuditAllCredentials)
	api.GET("/marketplace/credentials/:id/config", marketplaceHandler.GetCredentialConfig)
	api.PATCH("/marketplace/credentials/:id/config", marketplaceHandler.UpdateCredentialConfig)

	// Webhook subscription management — register/deregister our endpoint with the marketplace.
	// Supported channels: eBay, WooCommerce, Shopify, BigCommerce, TikTok.
	// Requires BACKEND_URL env var to be set to this server's public URL.
	api.POST("/marketplace/credentials/:id/webhooks/subscribe", orderWebhookHandler.SubscribeWebhooks)
	api.DELETE("/marketplace/credentials/:id/webhooks/unsubscribe", orderWebhookHandler.UnsubscribeWebhooks)
	api.GET("/marketplace/credentials/:id/webhooks/status", orderWebhookHandler.WebhookStatus)
	api.POST("/marketplace/import", marketplaceHandler.StartImport)
	api.GET("/marketplace/import/jobs", marketplaceHandler.ListImportJobs)
	api.GET("/marketplace/import/jobs/:id", marketplaceHandler.GetImportJob)
	api.POST("/marketplace/import/jobs/:id/cancel", marketplaceHandler.CancelImportJob)
	api.DELETE("/marketplace/import/jobs/:id", marketplaceHandler.DeleteImportJob)
	// Second-import matching flow
	api.POST("/marketplace/import/jobs/:id/analyze-matches", importMatchHandler.AnalyzeMatches)
	api.GET("/marketplace/import/jobs/:id/matches", importMatchHandler.GetMatches)
	api.POST("/marketplace/import/jobs/:id/matches/accept", importMatchHandler.AcceptMatches)
	api.POST("/marketplace/import/jobs/:id/matches/reject", importMatchHandler.RejectMatches)
	api.POST("/marketplace/import/jobs/:id/unmatched/import-new", importMatchHandler.ImportUnmatchedAsNew)
	// Tenant-wide pending review (Review Mappings page)
	api.GET("/marketplace/pending-review/count", importMatchHandler.GetPendingReviewCount)
	api.GET("/marketplace/pending-review", importMatchHandler.GetPendingReview)
	api.POST("/marketplace/listings", marketplaceHandler.CreateListing)
	api.GET("/marketplace/listings", marketplaceHandler.ListListings)
	api.GET("/marketplace/listings/unlisted", marketplaceHandler.ListUnlisted)
	api.GET("/listings/check-sku", skuCheckHandler.CheckSKU) // FLD-15
	api.GET("/marketplace/listings/:id/analytics", listingAnalyticsHandler.GetListingAnalytics) // USP-02 — before /:id
	api.GET("/marketplace/listings/:id", marketplaceHandler.GetListing)
	api.PATCH("/marketplace/listings/:id", marketplaceHandler.UpdateListing)
	api.DELETE("/marketplace/listings/:id", marketplaceHandler.DeleteListing)
	api.POST("/marketplace/listings/:id/publish", marketplaceHandler.PublishListing)
	api.POST("/marketplace/listings/:id/validate", marketplaceHandler.ValidateListing)
	api.POST("/marketplace/listings/bulk/publish", marketplaceHandler.BulkPublishListings)
	api.POST("/marketplace/listings/bulk/enrich", marketplaceHandler.BulkEnrichListings)
	api.POST("/marketplace/listings/bulk/delete", marketplaceHandler.BulkDeleteListings)
	api.POST("/marketplace/listings/bulk/revise/preview", marketplaceHandler.BulkRevisePreview) // USP-03 — must be before /revise
	api.POST("/marketplace/listings/bulk/revise", marketplaceHandler.BulkReviseListings)
	api.GET("/marketplace/adapters", marketplaceHandler.ListAdapters)
	api.GET("/marketplace/adapters/:id/fields", marketplaceHandler.GetAdapterFields)
	// Firestore-backed marketplace registry — used by the Connections page
	api.GET("/marketplace/registry", marketplaceHandler.GetRegistry)

	// TEMU
	temuGroup := api.Group("/temu")
	{
		temuGroup.POST("/categories/recommend", temuHandler.RecommendCategory)
		temuGroup.GET("/categories", temuHandler.GetCategories)
		temuGroup.GET("/category/path", temuHandler.GetCategoryPath)
		temuGroup.GET("/template", temuHandler.GetTemplate)
		temuGroup.GET("/shipping-templates", temuHandler.GetShippingTemplates)
		temuGroup.POST("/brand/trademark", temuHandler.LookupBrandTrademark)
		temuGroup.GET("/brands", temuHandler.ListBrands)
		temuGroup.GET("/brand-mappings", temuHandler.GetBrandMappings)
		temuGroup.PUT("/brand-mappings", temuHandler.SaveBrandMappings)
		temuGroup.GET("/brand-mappings/export", temuHandler.ExportBrandMappings)
		temuGroup.POST("/brand-mappings/import", temuHandler.ImportBrandMappings)
		temuGroup.GET("/compliance", temuHandler.GetCompliance)
		temuGroup.POST("/prepare", temuHandler.PrepareTemuListing)
		temuGroup.POST("/submit", temuHandler.SubmitTemuListing)
		temuGroup.GET("/schemas/list", temuSchemaHandler.ListSchemas)
		temuGroup.GET("/schemas/stats", temuSchemaHandler.Stats)
		temuGroup.POST("/schemas/sync", temuSchemaHandler.Sync)
		temuGroup.POST("/schemas/sync-missing-roots", temuSchemaHandler.SyncMissingRoots)
		temuGroup.POST("/schemas/resume", temuSchemaHandler.Resume)
		temuGroup.GET("/schemas/jobs", temuSchemaHandler.ListJobs)
		temuGroup.GET("/schemas/jobs/:jobId", temuSchemaHandler.GetJobStatus)
		temuGroup.POST("/schemas/jobs/:jobId/cancel", temuSchemaHandler.CancelJob)
		temuGroup.GET("/schemas/refresh-settings", temuSchemaHandler.GetRefreshSettings) // USP-04
		temuGroup.PUT("/schemas/refresh-settings", temuSchemaHandler.SaveRefreshSettings) // USP-04
		temuGroup.POST("/subscribe-webhook-events", temuHandler.SubscribeWebhookEvents)
		temuGroup.POST("/subscribe-webhook-events/all", temuHandler.SubscribeWebhookEventsAll)
	}

	// ── TIKTOK SHOP ──────────────────────────────────────────────────────────
	// OAuth callback must be outside the auth middleware to receive TikTok's redirect
	router.GET("/api/v1/tiktok/oauth/callback", tiktokHandler.OAuthCallback)

	tiktokGroup := api.Group("/tiktok")
	{
		tiktokGroup.GET("/oauth/login", tiktokHandler.OAuthLogin)
		tiktokGroup.GET("/shops", tiktokHandler.GetShops)
		tiktokGroup.GET("/categories", tiktokHandler.GetCategories)
		tiktokGroup.GET("/categories/:id/attributes", tiktokHandler.GetCategoryAttributes)
		tiktokGroup.GET("/brands", tiktokHandler.GetBrands)
		tiktokGroup.GET("/shipping-templates", tiktokHandler.GetShippingTemplates)
		tiktokGroup.GET("/shipping-providers", tiktokHandler.GetShippingProviders)
		tiktokGroup.GET("/warehouses", tiktokHandler.GetWarehouses)
		tiktokGroup.POST("/images/upload", tiktokHandler.UploadImage)
		tiktokGroup.POST("/prepare", tiktokHandler.PrepareListingDraft)
		tiktokGroup.POST("/submit", tiktokHandler.SubmitListing)
		tiktokGroup.PUT("/products/:id", tiktokHandler.UpdateProductListing)
		tiktokGroup.GET("/products", tiktokHandler.GetProducts)
		tiktokGroup.DELETE("/products/:id", tiktokHandler.DeleteProduct)
		// Orders
		tiktokGroup.POST("/orders/import", tiktokOrdersHandler.TriggerImport)
		tiktokGroup.GET("/orders", tiktokOrdersHandler.ListOrders)
		tiktokGroup.POST("/orders/:id/ship", tiktokOrdersHandler.PushTracking)
		tiktokGroup.POST("/orders/:id/cancel", tiktokOrdersHandler.CancelOrder)
		tiktokGroup.GET("/orders/reasons", tiktokOrdersHandler.GetCancelReasons)
	}

	// ── ETSY ──────────────────────────────────────────────────────────────────
	// OAuth callback must be outside the auth middleware to receive Etsy's redirect
	router.GET("/api/v1/etsy/oauth/callback", etsyHandler.OAuthCallback)
	router.GET("/api/v1/shopify/oauth/callback", shopifyHandler.OAuthCallback)
	router.GET("/api/v1/shopline/oauth/callback", shoplineHandler.OAuthCallback)
	router.GET("/api/v1/amazon/oauth/callback", amazonOAuthHandler.OAuthCallback)
	router.GET("/api/v1/amazon/connect", amazonOAuthHandler.PublicConnect)

	etsyGroup := api.Group("/etsy")
	{
		etsyGroup.GET("/oauth/login", etsyHandler.OAuthLogin)
		etsyGroup.GET("/shop", etsyHandler.GetShop)
		etsyGroup.GET("/taxonomy", etsyHandler.GetTaxonomy)
		etsyGroup.GET("/taxonomy/:id/properties", etsyHandler.GetTaxonomyProperties)
		etsyGroup.GET("/shipping-profiles", etsyHandler.GetShippingProfiles)
		etsyGroup.POST("/images/upload", etsyHandler.UploadImage)
		etsyGroup.POST("/prepare", etsyHandler.PrepareListingDraft)
		etsyGroup.POST("/submit", etsyHandler.SubmitListing)
		etsyGroup.PUT("/listings/:id", etsyHandler.UpdateProductListing)
		etsyGroup.DELETE("/listings/:id", etsyHandler.DeleteProductListing)
		etsyGroup.GET("/listings", etsyHandler.GetListings)
		// Orders
		etsyGroup.POST("/orders/import", etsyOrdersHandler.TriggerImport)
		etsyGroup.GET("/orders", etsyOrdersHandler.ListOrders)
		etsyGroup.POST("/orders/:id/ship", etsyOrdersHandler.PushTracking)
	}

	// ── WOOCOMMERCE ───────────────────────────────────────────────────────────
	// No OAuth — credentials (store_url, consumer_key, consumer_secret) are
	// saved directly after a test-connection pass.
	wooGroup := api.Group("/woocommerce")
	{
		wooGroup.POST("/connect", wooHandler.SaveCredential)
		wooGroup.POST("/test", wooHandler.TestConnection)
		wooGroup.GET("/categories", wooHandler.GetCategories)
		wooGroup.GET("/attributes", wooHandler.GetAttributes)
		wooGroup.POST("/prepare", wooHandler.PrepareListingDraft)
		wooGroup.POST("/submit", wooHandler.SubmitListing)
		wooGroup.PUT("/products/:id", wooHandler.UpdateProductListing)
		wooGroup.DELETE("/products/:id", wooHandler.DeleteProduct)
		wooGroup.GET("/products", wooHandler.GetProducts)
		// Orders
		wooGroup.POST("/orders/import", wooOrdersHandler.TriggerImport)
		wooGroup.GET("/orders", wooOrdersHandler.ListOrders)
		wooGroup.POST("/orders/:id/ship", wooOrdersHandler.PushTracking)
		wooGroup.POST("/orders/:id/status", wooOrdersHandler.UpdateOrderStatus)
	}

	// ── SHOPWIRED ─────────────────────────────────────────────────────────────
	shopwiredGroup := api.Group("/shopwired")
	{
		shopwiredGroup.POST("/test", shopwiredHandler.TestConnection)
		shopwiredGroup.POST("/credentials", shopwiredHandler.SaveCredential)
		shopwiredGroup.GET("/categories", shopwiredHandler.GetCategories)
		shopwiredGroup.GET("/brands", shopwiredHandler.GetBrands)
		shopwiredGroup.POST("/prepare", shopwiredHandler.PrepareListingDraft)
		shopwiredGroup.POST("/submit", shopwiredHandler.SubmitListing)
		shopwiredGroup.PUT("/products/:id", shopwiredHandler.UpdateProductListing)
		shopwiredGroup.DELETE("/products/:id", shopwiredHandler.DeleteProduct)
		shopwiredGroup.GET("/products", shopwiredHandler.GetProducts)
		shopwiredGroup.POST("/stock", shopwiredHandler.UpdateStock)
		shopwiredGroup.POST("/webhooks/register", shopwiredHandler.RegisterWebhooks)

		shopwiredGroup.POST("/orders/import", shopwiredOrdersHandler.ImportOrders)
		shopwiredGroup.GET("/orders", shopwiredOrdersHandler.GetOrders)
		shopwiredGroup.POST("/orders/:id/ship", shopwiredOrdersHandler.MarkShipped)
		shopwiredGroup.POST("/orders/:id/status", shopwiredOrdersHandler.UpdateOrderStatus)
	}
	walmartGroup := api.Group("/walmart")
	{
		walmartGroup.POST("/connect", walmartHandler.SaveCredential)
		walmartGroup.POST("/test", walmartHandler.TestConnection)
		walmartGroup.POST("/prepare", walmartHandler.PrepareListingDraft)
		walmartGroup.POST("/submit", walmartHandler.SubmitItemFeed)
		walmartGroup.GET("/feeds/:id", walmartHandler.GetFeedStatus)
		walmartGroup.PUT("/items/:sku/inventory", walmartHandler.UpdateInventory)
		walmartGroup.PUT("/items/:sku/price", walmartHandler.UpdatePrice)
		walmartGroup.DELETE("/items/:sku", walmartHandler.RetireItem)
		walmartGroup.GET("/items", walmartHandler.GetItems)
		// Orders
		walmartGroup.POST("/orders/import", walmartOrdersHandler.TriggerImport)
		walmartGroup.GET("/orders", walmartOrdersHandler.ListOrders)
		walmartGroup.POST("/orders/:id/ship", walmartOrdersHandler.PushTracking)
		walmartGroup.POST("/orders/:id/acknowledge", walmartOrdersHandler.AcknowledgeOrder)
	}

	// ── KAUFLAND ──────────────────────────────────────────────────────────────
	// No OAuth — credentials (client_key, secret_key) are saved directly
	// after a test-connection pass. HMAC-SHA256 auth per request.
	kauflandGroup := api.Group("/kaufland")
	{
		kauflandGroup.POST("/connect", kauflandHandler.SaveCredential)
		kauflandGroup.POST("/test", kauflandHandler.TestConnection)
		kauflandGroup.GET("/categories", kauflandHandler.GetCategories)
		kauflandGroup.POST("/prepare", kauflandHandler.PrepareListingDraft)
		kauflandGroup.POST("/submit", kauflandHandler.SubmitUnit)
		kauflandGroup.PATCH("/units/:id", kauflandHandler.UpdateUnit)
		kauflandGroup.DELETE("/units/:id", kauflandHandler.DeleteUnit)
		kauflandGroup.GET("/units", kauflandHandler.GetUnits)
		// Orders
		kauflandGroup.POST("/orders/import", kauflandOrdersHandler.TriggerImport)
		kauflandGroup.GET("/orders", kauflandOrdersHandler.ListOrders)
		kauflandGroup.POST("/orders/:id/ship", kauflandOrdersHandler.PushTracking)
		kauflandGroup.POST("/orders/:id/status", kauflandOrdersHandler.UpdateOrderStatus)
	}

	// ── MAGENTO 2 ─────────────────────────────────────────────────────────────
	// No OAuth — credentials (store_url, integration_token) are saved directly
	// after a test-connection pass. Bearer token auth per request.
	magentoGroup := api.Group("/magento")
	{
		magentoGroup.POST("/connect", magentoHandler.SaveCredential)
		magentoGroup.POST("/test", magentoHandler.TestConnection)
		magentoGroup.GET("/categories", magentoHandler.GetCategories)
		magentoGroup.POST("/prepare", magentoHandler.PrepareListingDraft)
		magentoGroup.POST("/submit", magentoHandler.SubmitListing)
		magentoGroup.PUT("/products/:sku", magentoHandler.UpdateProductListing)
		magentoGroup.DELETE("/products/:sku", magentoHandler.DeleteProduct)
		magentoGroup.GET("/products", magentoHandler.GetProducts)
		// Orders
		magentoGroup.POST("/orders/import", magentoOrdersHandler.TriggerImport)
		magentoGroup.GET("/orders", magentoOrdersHandler.ListOrders)
		magentoGroup.POST("/orders/:id/ship", magentoOrdersHandler.PushTracking)
		magentoGroup.POST("/orders/:id/status", magentoOrdersHandler.UpdateOrderStatus)
	}

	// ── BIGCOMMERCE ───────────────────────────────────────────────────────────
	// No OAuth — credentials (store_hash, client_id, access_token) saved directly
	// after a test-connection pass. X-Auth-Token header auth per request.
	bigcommerceGroup := api.Group("/bigcommerce")
	{
		bigcommerceGroup.POST("/connect", bigcommerceHandler.SaveCredential)
		bigcommerceGroup.POST("/test", bigcommerceHandler.TestConnection)
		bigcommerceGroup.GET("/categories", bigcommerceHandler.GetCategories)
		bigcommerceGroup.POST("/prepare", bigcommerceHandler.PrepareListingDraft)
		bigcommerceGroup.POST("/submit", bigcommerceHandler.SubmitListing)
		bigcommerceGroup.PUT("/products/:id", bigcommerceHandler.UpdateProductListing)
		bigcommerceGroup.DELETE("/products/:id", bigcommerceHandler.DeleteProduct)
		bigcommerceGroup.GET("/products", bigcommerceHandler.GetProducts)
		// Orders
		bigcommerceGroup.POST("/orders/import", bigcommerceOrdersHandler.TriggerImport)
		bigcommerceGroup.GET("/orders", bigcommerceOrdersHandler.ListOrders)
		bigcommerceGroup.POST("/orders/:id/ship", bigcommerceOrdersHandler.PushTracking)
		bigcommerceGroup.POST("/orders/:id/status", bigcommerceOrdersHandler.UpdateOrderStatus)
	}

	// ── ONBUY ─────────────────────────────────────────────────────────────────
	onbuyGroup := api.Group("/onbuy")
	{
		onbuyGroup.POST("/connect", onbuyHandler.SaveCredential)
		onbuyGroup.POST("/test", onbuyHandler.TestConnection)
		onbuyGroup.GET("/categories", onbuyHandler.GetCategories)
		onbuyGroup.GET("/conditions", onbuyHandler.GetConditions)
		onbuyGroup.POST("/prepare", onbuyHandler.PrepareListingDraft)
		onbuyGroup.POST("/submit", onbuyHandler.SubmitListing)
		onbuyGroup.PUT("/listings/:id", onbuyHandler.UpdateListing)
		onbuyGroup.DELETE("/listings/:id", onbuyHandler.DeleteListing)
		onbuyGroup.GET("/listings", onbuyHandler.GetListings)
		// Orders
		onbuyGroup.POST("/orders/import", onbuyOrdersHandler.TriggerImport)
		onbuyGroup.GET("/orders", onbuyOrdersHandler.ListOrders)
		onbuyGroup.POST("/orders/:id/ship", onbuyOrdersHandler.PushTracking)
		onbuyGroup.POST("/orders/:id/ack", onbuyOrdersHandler.AcknowledgeOrder)
	}

	// ── BLUEPARK ─────────────────────────────────────────────────────────────
	blueparkGroup := api.Group("/bluepark")
	{
		blueparkGroup.POST("/connect", blueparkHandler.SaveCredential)
		// Listings — specific routes BEFORE parameterised routes (Gin ordering rule)
		blueparkGroup.POST("/listings/prepare", blueparkHandler.PrepareListingDraft)
		blueparkGroup.POST("/listings/submit", blueparkHandler.SubmitListing)
		// Orders
		blueparkGroup.GET("/orders/import", blueparkHandler.ImportOrders)
	}

	// ── WISH ─────────────────────────────────────────────────────────────────
	wishGroup := api.Group("/wish")
	{
		wishGroup.POST("/connect", wishHandler.SaveCredential)
		// Listings
		wishGroup.POST("/listings/prepare", wishHandler.PrepareListingDraft)
		wishGroup.POST("/listings/submit", wishHandler.SubmitListing)
		// Orders
		wishGroup.GET("/orders/import", wishHandler.ImportOrders)
	}

	// ── EXTRACT (IMP-01, CLM-01, CLM-02) ─────────────────────────────────────
	extractGroup := api.Group("/extract")
	{
		extractGroup.GET("/channels", extractHandler.ListExtractableChannels)
		extractGroup.GET("/:channel/listings", extractHandler.BrowseChannelListings)
		// Specific route must come before parameterised — /listings/extract before /:id/link
		extractGroup.POST("/:channel/listings/extract", extractHandler.ExtractListings)
		extractGroup.POST("/listings/:listing_id/link", extractHandler.LinkListingToProduct)
	}

	// ── MIRAKL-POWERED MARKETPLACES ─────────────────────────────────────────
	// One route group serves ALL Mirakl instances (Tesco, B&Q, Superdrug, etc.)
	// The credential_id param/header identifies which marketplace + account to use.
	miraklGroup := api.Group("/mirakl")
	{
		// Connection & account
		miraklGroup.GET("/health", miraklHandler.HealthCheck)
		miraklGroup.GET("/shop", miraklHandler.GetShopInfo)

		// Catalog / setup
		miraklGroup.GET("/categories", miraklHandler.GetCategories)
		miraklGroup.GET("/carriers", miraklHandler.GetCarriers)

		// Offers (listings: price + stock)
		miraklGroup.GET("/offers", miraklHandler.ListOffers)
		miraklGroup.POST("/offers/upsert", miraklHandler.UpsertOffers)
		miraklGroup.POST("/offers/delete", miraklHandler.DeleteOffers)

		// Products (catalog submission)
		miraklGroup.POST("/products/submit", miraklHandler.SubmitProducts)
		miraklGroup.GET("/products/import/:import_id", miraklHandler.GetImportStatus)

		// Invoices / accounting
		miraklGroup.GET("/invoices", miraklHandler.ListInvoices)

		// Orders
		miraklGroup.POST("/orders/import", miraklOrdersHandler.TriggerImport)
		miraklGroup.GET("/orders", miraklOrdersHandler.ListOrders)
		miraklGroup.POST("/orders/:id/accept", miraklOrdersHandler.AcceptOrder)
		miraklGroup.POST("/orders/:id/tracking", miraklOrdersHandler.PushTracking)
		miraklGroup.POST("/orders/:id/refund", miraklOrdersHandler.RefundOrder)
		miraklGroup.POST("/orders/:id/cancel", miraklOrdersHandler.CancelOrder)
	}

	// EBAY
	ebayGroup := api.Group("/ebay")
	{
		ebayGroup.GET("/oauth/login", ebayHandler.OAuthLogin)
		ebayGroup.GET("/inventory", ebayHandler.ListInventory)
		ebayGroup.GET("/inventory/:sku", ebayHandler.GetInventoryItem)
		ebayGroup.GET("/offers/:sku", ebayHandler.GetOffers)
		ebayGroup.GET("/policies", ebayHandler.GetPolicies)
		ebayGroup.GET("/locations", ebayHandler.GetLocations)
		ebayGroup.GET("/categories/suggest", ebayHandler.SuggestCategories)
		ebayGroup.GET("/categories/aspects", ebayHandler.GetItemAspects)
		ebayGroup.GET("/catalog/search", ebayHandler.CatalogSearch)
		ebayGroup.POST("/test", ebayHandler.TestConnection)
		ebayGroup.POST("/prepare", ebayHandler.PrepareEbayListing)
		ebayGroup.POST("/submit", ebayHandler.SubmitEbayListing)
		ebayGroup.GET("/schemas/list", ebaySchemaHandler.ListSchemas)
		ebayGroup.GET("/schemas/stats", ebaySchemaHandler.Stats)
		ebayGroup.POST("/schemas/sync", ebaySchemaHandler.Sync)
		ebayGroup.GET("/schemas/jobs", ebaySchemaHandler.ListJobs)
		ebayGroup.GET("/schemas/jobs/:jobId", ebaySchemaHandler.GetJobStatus)
		ebayGroup.POST("/schemas/jobs/:jobId/cancel", ebaySchemaHandler.CancelJob)
		ebayGroup.GET("/schemas/refresh-settings", ebaySchemaHandler.GetRefreshSettings) // USP-04
		ebayGroup.PUT("/schemas/refresh-settings", ebaySchemaHandler.SaveRefreshSettings) // USP-04
		// Browse Enrichment
		ebayGroup.POST("/enrich/product", ebayEnrichHandler.EnrichProduct)
		ebayGroup.POST("/enrich/bulk", ebayEnrichHandler.BulkEnrich)
		ebayGroup.GET("/enrich/status", ebayEnrichHandler.EnrichStatus)

		// eBay order download (manual trigger from UI or channel-specific call)
		ebayGroup.POST("/orders/import", ebayOrdersHandler.TriggerImport)
	}
	// eBay enrichment internal task callback (Cloud Tasks, no tenant middleware)
	router.POST("/api/v1/internal/ebay/enrich/task", ebayEnrichHandler.ProcessTask)

	// Order sync task callback — called by Cloud Tasks for each credential's poll cycle.
	// Secured by X-CloudTasks-QueueName header (set automatically by Cloud Tasks on GCP).
	router.POST("/api/v1/internal/orders/sync-task", orderSyncHandler.ProcessSyncTask)

	// Internal order orchestration — callable by Cloud Scheduler or operators.
	// Protected by X-Internal-Secret header (set INTERNAL_SECRET env var).
	// The in-process poller also runs every 15 min without needing this route.
	router.POST("/internal/orders/orchestrate", orchestratorHandler.Orchestrate)

	// Internal keyword enrichment — called by import-enrich Cloud Run service
	// after GetCatalogItem succeeds. OIDC-authenticated (Cloud Run service account).
	// Runs EnrichFromCatalogData + RefreshFromAmazonAdsAPI in background goroutine.
	// DataForSEO is intentionally NOT triggered here.
	router.POST("/internal/keyword-intelligence/enrich", keywordIntelligenceHandler.EnrichFromImport)

	// ── ORDER WEBHOOKS ────────────────────────────────────────────────────────
	// Public endpoints — no Firebase auth. Each verifies its own signature.
	// eBay: single global endpoint for all tenants (seller matched by username).
	// Others: tenant+cred embedded as query params in the URL.
	router.POST("/webhooks/orders/ebay", orderWebhookHandler.EbayWebhook)
	router.POST("/webhooks/orders/woocommerce", orderWebhookHandler.WooCommerceWebhook)
	router.POST("/webhooks/orders/shopify", orderWebhookHandler.ShopifyWebhook)
	router.POST("/webhooks/orders/shopline", orderWebhookHandler.ShoplineWebhook)
	router.POST("/webhooks/orders/bigcommerce", orderWebhookHandler.BigCommerceWebhook)
	router.POST("/webhooks/orders/tiktok", orderWebhookHandler.TikTokOrderWebhook)

	// ── AMAZON MESSAGING WEBHOOKS ─────────────────────────────────────────────
	// SNS subscription confirmation (GET) and message delivery (POST).
	// Public — no Firebase auth. Signature verified via SNS certificate.
	router.GET("/webhooks/messages/amazon", amazonMessagingWebhookHandler.HandleAmazonMessagingWebhook)
	router.POST("/webhooks/messages/amazon", amazonMessagingWebhookHandler.HandleAmazonMessagingWebhook)

	// ── TEMU MESSAGE REDIRECT ─────────────────────────────────────────────────
	// Temu has no messaging API — provide a redirect URL to Seller Centre.
	router.GET("/webhooks/messages/temu", func(c *gin.Context) {
		c.Redirect(302, "https://seller.temu.com/messages")
	})

	// ── TEMU AFTER-SALES & CANCEL WEBHOOKS ───────────────────────────────────────
	// Receives encrypted push notifications for refund/return/cancel events.
	// Signature: HMAC-SHA256. Payload: AES/CBC encrypted with app_secret.
	router.POST("/webhooks/temu/aftersales", orderWebhookHandler.TemuAfterSalesWebhook)

	// SHOPIFY
	shopifyGroup := api.Group("/shopify")
	{
		// OAuth (public app flow)
		shopifyGroup.GET("/oauth/login", shopifyHandler.OAuthLogin)
		// Note: /oauth/callback is registered outside auth middleware below

		// Connection
		shopifyGroup.GET("/test", shopifyHandler.TestConnection)

		// Listing (PRC-01)
		shopifyGroup.POST("/prepare", shopifyListingHandler.PrepareShopifyListing)
		shopifyGroup.POST("/submit", shopifyListingHandler.SubmitShopifyListing)

		// Orders
		shopifyGroup.POST("/orders/import", shopifyHandler.ImportOrders)
		shopifyGroup.GET("/orders", shopifyHandler.GetOrders)
		shopifyGroup.POST("/orders/:id/ship", shopifyHandler.MarkShipped)

		// Stock
		shopifyGroup.POST("/stock", shopifyHandler.UpdateStock)

		// Store reference data (loaded before listing create)
		shopifyGroup.GET("/locations", shopifyHandler.GetLocations)
		shopifyGroup.GET("/publications", shopifyHandler.GetPublications)
		shopifyGroup.GET("/tags", shopifyHandler.GetTags)
		shopifyGroup.GET("/types", shopifyHandler.GetTypes)
		shopifyGroup.GET("/collections", shopifyHandler.GetCollections)
		shopifyGroup.GET("/metafield-defs", shopifyHandler.GetMetafieldDefs)
		shopifyGroup.GET("/categories", shopifyHandler.GetCategories)

		// Webhooks
		shopifyGroup.POST("/webhooks/register", shopifyHandler.RegisterWebhooks)
	}

	// SHOPLINE
	shoplineGroup := api.Group("/shopline")
	{
		// OAuth (public app flow)
		shoplineGroup.GET("/oauth/login", shoplineHandler.OAuthLogin)
		// Note: /oauth/callback is registered outside auth middleware above

		// Connection
		shoplineGroup.GET("/test", shoplineHandler.TestConnection)

		// Listing
		shoplineGroup.POST("/prepare", shoplineListingHandler.PrepareShoplineListing)
		shoplineGroup.POST("/submit", shoplineListingHandler.SubmitShoplineListing)

		// Orders
		shoplineGroup.POST("/orders/import", shoplineHandler.ImportOrders)
		shoplineGroup.GET("/orders", shoplineHandler.GetOrders)
		shoplineGroup.POST("/orders/:id/ship", shoplineHandler.MarkShipped)

		// Stock
		shoplineGroup.POST("/stock", shoplineHandler.UpdateStock)

		// Store reference data (loaded before listing create)
		shoplineGroup.GET("/locations", shoplineHandler.GetLocations)
		shoplineGroup.GET("/channels", shoplineHandler.GetChannels)
		shoplineGroup.GET("/tags", shoplineHandler.GetTags)
		shoplineGroup.GET("/types", shoplineHandler.GetTypes)
		shoplineGroup.GET("/collections", shoplineHandler.GetCollections)
		shoplineGroup.GET("/categories", shoplineHandler.GetCategories)

		// Webhooks
		shoplineGroup.POST("/webhooks/register", shoplineHandler.RegisterWebhooks)
	}

	// AMAZON
	amazonGroup := api.Group("/amazon")
	{
		// Listing, schema, catalog routes (OAuth handled by /amazon/oauth/login and /amazon/oauth/callback)
		amazonGroup.GET("/product-types/search", amazonHandler.SearchProductTypes)
		amazonGroup.GET("/product-types/definition", amazonHandler.GetProductTypeDefinition)
		amazonGroup.GET("/catalog/search", amazonHandler.SearchCatalog)
		amazonGroup.POST("/debug-enrich", amazonHandler.DebugEnrich) // TEMPORARY debug endpoint
		amazonGroup.POST("/prepare", amazonHandler.PrepareAmazonListing)
		amazonGroup.POST("/submit", amazonHandler.SubmitAmazonListing)
		amazonGroup.GET("/restrictions", amazonHandler.CheckRestrictions)
		amazonGroup.POST("/validate", amazonHandler.ValidateListing)
		amazonGroup.GET("/listing", amazonHandler.GetListing)
		amazonGroup.GET("/schemas/list", amazonSchemaHandler.ListSchemas)
		amazonGroup.POST("/schemas/download", amazonSchemaHandler.DownloadSchema)
		amazonGroup.POST("/schemas/download-all", amazonSchemaHandler.DownloadAll)
		amazonGroup.GET("/schemas/jobs", amazonSchemaHandler.ListJobs)
		amazonGroup.GET("/schemas/jobs/:jobId", amazonSchemaHandler.GetJobStatus)
		amazonGroup.POST("/schemas/jobs/:jobId/cancel", amazonSchemaHandler.CancelJob)
		amazonGroup.GET("/schemas/:productType", amazonSchemaHandler.GetSchema)
		amazonGroup.POST("/schemas/:productType/field-config", amazonSchemaHandler.SaveFieldConfig)
		amazonGroup.DELETE("/schemas/:productType", amazonSchemaHandler.DeleteSchema)
		// ENH-02: auto-refresh settings
		amazonGroup.GET("/schemas/refresh-settings", amazonSchemaHandler.GetRefreshSettings)
		amazonGroup.PUT("/schemas/refresh-settings", amazonSchemaHandler.SaveRefreshSettings)
	}

	// AMAZON OAuth routes (OAuthLogin — callback registered outside auth middleware above)
	amazonOAuthGroup := api.Group("/amazon")
	{
		amazonOAuthGroup.GET("/oauth/login", amazonOAuthHandler.OAuthLogin)
	}

	// AI
	aiGroup := api.Group("/ai")
	{
		aiGroup.GET("/status", aiHandler.Status)
		aiGroup.POST("/generate", aiHandler.GenerateSingle)
		aiGroup.POST("/generate-with-schema", aiHandler.GenerateWithSchema)
		aiGroup.POST("/generate/bulk", aiHandler.GenerateBulk)
		aiGroup.GET("/generate/jobs", aiHandler.ListJobs)
		aiGroup.GET("/generate/jobs/:id", aiHandler.GetJob)
		aiGroup.POST("/generate/apply", aiHandler.ApplyGenerated)
		aiGroup.POST("/prompt", aiHandler.PromptDirect) // internal free-form proxy

		// AI Consolidation
		aiGroup.POST("/consolidate/product", aiConsolidationHandler.ConsolidateProduct)
		aiGroup.POST("/consolidate/bulk", aiConsolidationHandler.BulkConsolidate)
		aiGroup.GET("/consolidate/jobs", aiConsolidationHandler.ListJobs)

		// AI Credits
		aiGroup.GET("/credits", settingsHandler.GetAICredits)
		aiGroup.POST("/credits/consume", settingsHandler.ConsumeCredits)
		aiGroup.POST("/credits/purchase", settingsHandler.PurchaseCreditPack)
		aiGroup.GET("/credits/packs", settingsHandler.ListCreditPacks)

	}

	// MODULE E - ORDER MANAGEMENT
	api.GET("/orders", orderHandler.ListOrders)
	api.GET("/orders/stats", orderHandler.GetOrderStats)
	api.GET("/orders/:id", orderHandler.GetOrder)
	api.GET("/orders/:id/lines", orderHandler.GetOrderLines)
	api.PATCH("/orders/:id/status", orderHandler.UpdateOrderStatus)
	api.POST("/orders/import", orderHandler.ImportOrders)
	api.POST("/orders/import/now", orchestratorHandler.ImportNow)
	api.GET("/orders/import/jobs", orderHandler.ListImportJobs)
	api.GET("/orders/import/jobs/:id", orderHandler.GetImportJob)
	api.POST("/orders/bulk/status", orderHandler.BulkUpdateStatus)
	
	// Order holds
	api.POST("/orders/hold", orderActionsHandler.HoldOrders)
	api.POST("/orders/hold/release", orderActionsHandler.ReleaseHolds)
	
	// Order locks
	api.POST("/orders/lock", orderActionsHandler.LockOrders)
	api.POST("/orders/lock/release", orderActionsHandler.UnlockOrders)
	
	// Order tags
	api.POST("/orders/tags", orderActionsHandler.AddTagToOrders)
	api.DELETE("/orders/tags", orderActionsHandler.RemoveTagFromOrders)
	api.GET("/orders/:id/tags", orderActionsHandler.GetOrderTags)

	// Order notes
	api.POST("/orders/:id/notes", orderActionsHandler.AddNoteToOrder)
	api.GET("/orders/:id/notes", orderActionsHandler.GetOrderNotes)

	// Task 10: Invoice print status tracking
	api.POST("/orders/:id/mark-invoice-printed", orderActionsHandler.MarkInvoicePrinted)

	// ── Actions Menu Extended Routes ──────────────────────────────────────────
	// Organise
	api.GET("/orders/organise/folders", orderActionsExtendedHandler.ListFolders)
	api.POST("/orders/organise/folders", orderActionsExtendedHandler.AssignFolders)
	api.POST("/orders/organise/folders/create", orderActionsExtendedHandler.CreateFolder)
	api.POST("/orders/organise/identifiers", orderActionsExtendedHandler.AssignIdentifiers)
	api.POST("/orders/organise/location", orderActionsExtendedHandler.MoveToLocation)
	api.POST("/orders/organise/fulfilment-center", orderActionsExtendedHandler.MoveToFulfilmentCenter)
	// Items
	api.POST("/orders/items/batch-assign", orderActionsExtendedHandler.BatchAssignment)
	api.POST("/orders/items/auto-assign-batches", orderActionsExtendedHandler.AutoAssignBatches)
	api.POST("/orders/items/clear-batches", orderActionsExtendedHandler.ClearBatches)
	api.POST("/orders/items/link-unlinked", orderActionsExtendedHandler.LinkUnlinkedItems)
	api.POST("/orders/items/add-to-po", orderActionsExtendedHandler.AddItemsToPurchaseOrder)
	// Shipping
	api.POST("/orders/shipping/change-service", orderActionsExtendedHandler.ChangeShippingService)
	api.POST("/orders/shipping/get-quotes", orderActionsExtendedHandler.GetShippingQuotes)
	api.POST("/orders/shipping/cancel-label", orderActionsExtendedHandler.CancelShippingLabel)
	api.POST("/orders/:id/shipping/split-packaging", orderActionsExtendedHandler.SplitPackaging)
	api.POST("/orders/shipping/change-dispatch-date", orderActionsExtendedHandler.ChangeDispatchDate)
	api.POST("/orders/shipping/change-delivery-dates", orderActionsExtendedHandler.ChangeDeliveryDates)
	// Process
	api.POST("/orders/:id/process", orderActionsExtendedHandler.ProcessOrder)
	api.POST("/orders/batch-process", orderActionsExtendedHandler.BatchProcessOrders)
	// Other Actions
	api.GET("/orders/:id/notes/full", orderActionsExtendedHandler.GetOrderNotesFull)
	api.DELETE("/orders/:id/notes/:note_id", orderActionsExtendedHandler.DeleteOrderNote)
	api.PATCH("/orders/:id/notes/:note_id", orderActionsExtendedHandler.UpdateOrderNote)
	api.GET("/orders/:id/xml", orderActionsExtendedHandler.GetOrderXML)
	api.DELETE("/orders/:id", orderActionsExtendedHandler.DeleteOrder)
	api.POST("/orders/run-rules", orderActionsExtendedHandler.RunRulesEngine)

	// SEARCH
	api.GET("/search/products", searchHandler.SearchProducts)
	api.GET("/search/listings", searchHandler.SearchListings)
	api.POST("/search/sync", searchHandler.SyncAll)
	api.POST("/search/reconnect", searchHandler.Reconnect)
	router.GET("/api/v1/search/health", searchHandler.Health)
	router.POST("/api/v1/admin/search/restart-vm", searchHandler.RestartTypesenseVM)
	
	// MODULE D - INVENTORY MANAGEMENT
	// Legacy inventory stats (old flat model)
	api.GET("/inventory/combined", inventoryHandler.GetCombinedStock)
	api.GET("/inventory/stats", inventoryHandler.GetInventoryStats)

	// Reservations (called automatically by order import)
	api.POST("/inventory/reservations", inventoryHandler.CreateReservation)
	api.POST("/inventory/reservations/:reservation_id/release", inventoryHandler.ReleaseReservation)

	// New warehouse location tree
	api.GET("/locations", warehouseLocationHandler.ListLocations)
	api.POST("/locations", warehouseLocationHandler.CreateLocation)
	api.PUT("/locations/:id", warehouseLocationHandler.UpdateLocation)
	api.DELETE("/locations/:id", warehouseLocationHandler.DeleteLocation)
	api.GET("/locations/:id/children", warehouseLocationHandler.GetLocationChildren)

	// New inventory endpoints — static paths MUST be registered before /:product_id
	api.GET("/inventory", warehouseLocationHandler.GetInventoryV2)
	api.GET("/inventory/adjustments", warehouseLocationHandler.GetAdjustments)
	api.POST("/inventory/adjust", warehouseLocationHandler.AdjustStockV2)
	api.POST("/inventory/transfer", warehouseLocationHandler.TransferStock)
	api.POST("/inventory/import/basic", warehouseLocationHandler.ImportBasicInventory)
	// Parameterised route last
	api.GET("/inventory/:product_id", warehouseLocationHandler.GetProductInventory)
	
	// MODULE F - DISPATCH & SHIPPING
	dispatchGroup := api.Group("/dispatch")
	{
		// Carrier management
		dispatchGroup.GET("/carriers", dispatchHandler.ListCarriers)
		dispatchGroup.GET("/carriers/configured", dispatchHandler.ListCarriersWithStatus)
		dispatchGroup.GET("/carriers/:carrier_id/services", dispatchHandler.GetCarrierServices)
		dispatchGroup.GET("/carriers/:carrier_id/credentials", dispatchHandler.GetCarrierCredentialStatus)
		dispatchGroup.POST("/carriers/:carrier_id/credentials", dispatchHandler.SaveCarrierCredentials)
		dispatchGroup.DELETE("/carriers/:carrier_id/credentials", dispatchHandler.DeleteCarrierCredentials)
		dispatchGroup.POST("/carriers/:carrier_id/test", dispatchHandler.TestCarrierConnection)

		// Rate shopping
		dispatchGroup.POST("/rates", dispatchHandler.GetRates)

		// Shipment operations
		dispatchGroup.POST("/shipments", dispatchHandler.CreateShipment)
		dispatchGroup.GET("/shipments", dispatchHandler.ListShipments)
		dispatchGroup.GET("/shipments/:shipment_id", dispatchHandler.GetShipment)
		dispatchGroup.GET("/shipments/:shipment_id/tracking", dispatchHandler.GetTracking)
		dispatchGroup.DELETE("/shipments/:shipment_id", dispatchHandler.VoidShipment)

		// Carrier manifest / end-of-day
		dispatchGroup.POST("/manifest", dispatchHandler.CreateManifest)
		dispatchGroup.GET("/manifest/history", dispatchHandler.ListManifests)
		dispatchGroup.GET("/manifest/:manifest_id", dispatchHandler.GetManifest)

		// Session 3 — Dispatch extensions
		dispatchGroup.GET("/sla-summary", dispatchExtHandler.GetSLASummary)
		dispatchGroup.POST("/address-validate", dispatchExtHandler.ValidateAddress)
		dispatchGroup.POST("/address-validate-bulk", dispatchExtHandler.ValidateAddressBulk)
		dispatchGroup.POST("/bulk-dispatch", dispatchExtHandler.BulkDispatch)
		dispatchGroup.POST("/orders/:order_id/dangerous-goods-check", dispatchExtHandler.CheckDangerousGoods)
		dispatchGroup.POST("/shipments/:shipment_id/writeback", dispatchExtHandler.WritebackTracking)
		dispatchGroup.POST("/shipments/:shipment_id/dispatch-email", dispatchExtHandler.TriggerDispatchEmail)
		dispatchGroup.GET("/shipments/:shipment_id/reprint", dispatchExtHandler.ReprintLabel)
		dispatchGroup.GET("/exceptions", dispatchExtHandler.ListExceptions)
		dispatchGroup.POST("/exceptions/:exception_id/acknowledge", dispatchExtHandler.AcknowledgeException)
		dispatchGroup.GET("/packaging-rules", dispatchExtHandler.ListPackagingRules)
		dispatchGroup.POST("/packaging-rules", dispatchExtHandler.CreatePackagingRule)
		dispatchGroup.PUT("/packaging-rules/:rule_id", dispatchExtHandler.UpdatePackagingRule)
		dispatchGroup.DELETE("/packaging-rules/:rule_id", dispatchExtHandler.DeletePackagingRule)
		dispatchGroup.POST("/packaging-rules/apply", dispatchExtHandler.ApplyPackagingRules)
		dispatchGroup.GET("/shipping-rules", dispatchExtHandler.ListShippingRules)
		dispatchGroup.POST("/shipping-rules", dispatchExtHandler.CreateShippingRule)
		dispatchGroup.PUT("/shipping-rules/:rule_id", dispatchExtHandler.UpdateShippingRule)
		dispatchGroup.DELETE("/shipping-rules/:rule_id", dispatchExtHandler.DeleteShippingRule)

		// Evri tracking sync
		dispatchGroup.POST("/shipments/:shipment_id/sync-tracking", trackingSyncHandler.SyncShipmentTracking)
		dispatchGroup.GET("/shipments/:shipment_id/tracking-detail", trackingSyncHandler.GetShipmentTracking)

		// Shipping templates
		dispatchGroup.GET("/shipping-templates", shippingTemplateHandler.ListTemplates)
		dispatchGroup.POST("/shipping-templates", shippingTemplateHandler.CreateTemplate)
		dispatchGroup.GET("/shipping-templates/:id", shippingTemplateHandler.GetTemplate)
		dispatchGroup.PUT("/shipping-templates/:id", shippingTemplateHandler.UpdateTemplate)
		dispatchGroup.DELETE("/shipping-templates/:id", shippingTemplateHandler.DeleteTemplate)
		dispatchGroup.POST("/shipping-templates/:id/render", shippingTemplateHandler.RenderTemplate)

		// Customs profiles
		dispatchGroup.GET("/customs-profiles", customsProfileHandler.ListProfiles)
		dispatchGroup.POST("/customs-profiles", customsProfileHandler.CreateProfile)
		dispatchGroup.GET("/customs-profiles/:id", customsProfileHandler.GetProfile)
		dispatchGroup.PUT("/customs-profiles/:id", customsProfileHandler.UpdateProfile)
		dispatchGroup.DELETE("/customs-profiles/:id", customsProfileHandler.DeleteProfile)
	}

	// MODULE G - WORKFLOW ENGINE
	workflowGroup := api.Group("/workflows")
	{
		// CRUD
		workflowGroup.GET("", workflowHandler.ListWorkflows)
		workflowGroup.POST("", workflowHandler.CreateWorkflow)
		workflowGroup.GET("/:id", workflowHandler.GetWorkflow)
		workflowGroup.PATCH("/:id", workflowHandler.UpdateWorkflow)
		workflowGroup.DELETE("/:id", workflowHandler.DeleteWorkflow)

		// Status operations
		workflowGroup.POST("/:id/activate", workflowHandler.ActivateWorkflow)
		workflowGroup.POST("/:id/pause", workflowHandler.PauseWorkflow)
		workflowGroup.POST("/:id/duplicate", workflowHandler.DuplicateWorkflow)

		// Testing
		workflowGroup.POST("/:id/test", workflowHandler.TestWorkflow)
		workflowGroup.POST("/simulate", workflowHandler.SimulateOrder)

		// Execution history
		workflowGroup.GET("/:id/executions", workflowHandler.GetExecutions)
		workflowGroup.GET("/executions/:id", workflowHandler.GetExecution)

		// Bulk operations
		workflowGroup.PATCH("/reorder", workflowHandler.ReorderWorkflows)
		workflowGroup.POST("/bulk/activate", workflowHandler.BulkActivate)
		workflowGroup.POST("/bulk/pause", workflowHandler.BulkPause)
	}

	// Workflow processing on an order (also accessible from orders group)
	api.POST("/orders/:id/process-workflows", workflowHandler.ProcessOrderWorkflows)

	// MODULE G EXTENSION — AUTOMATION RULE ENGINE
	automationGroup := api.Group("/automation")
	{
		// List & create (no param — must come first)
		automationGroup.GET("/rules", automationHandler.ListRules)
		automationGroup.POST("/rules", automationHandler.CreateRule)

		// validate and test MUST be registered before /:id or Gin captures them as the id param
		automationGroup.POST("/rules/validate", automationHandler.ValidateRule)
		automationGroup.POST("/rules/test", automationHandler.TestRule)

		// Parameterised routes after all fixed paths
		automationGroup.GET("/rules/:id", automationHandler.GetRule)
		automationGroup.PUT("/rules/:id", automationHandler.UpdateRule)
		automationGroup.DELETE("/rules/:id", automationHandler.DeleteRule)
		automationGroup.PATCH("/rules/:id/toggle", automationHandler.ToggleRule)
		automationGroup.POST("/rules/:id/duplicate", automationHandler.DuplicateRule)
		automationGroup.GET("/rules/:id/history", automationHandler.GetRuleHistory)

		// Manual trigger & metadata
		automationGroup.POST("/trigger/:event", automationHandler.TriggerEvent)
		automationGroup.GET("/actions", automationHandler.ListActions)
		automationGroup.GET("/fields", automationHandler.ListFields)
	}

	// MODULE G - FULFILMENT SOURCES
	api.GET("/fulfilment-sources", fulfilmentSourceHandler.ListSources)
	api.POST("/fulfilment-sources", fulfilmentSourceHandler.CreateSource)
	api.GET("/fulfilment-sources/:id", fulfilmentSourceHandler.GetSource)
	api.PATCH("/fulfilment-sources/:id", fulfilmentSourceHandler.UpdateSource)
	api.DELETE("/fulfilment-sources/:id", fulfilmentSourceHandler.DeleteSource)
	api.POST("/fulfilment-sources/:id/set-default", fulfilmentSourceHandler.SetDefaultSource)

	// MODULE G - SUPPLIERS
	api.GET("/suppliers", supplierHandler.ListSuppliers)
	api.POST("/suppliers", supplierHandler.CreateSupplier)
	api.GET("/suppliers/:id", supplierHandler.GetSupplier)
	api.PUT("/suppliers/:id", supplierHandler.UpdateSupplier)
	api.DELETE("/suppliers/:id", supplierHandler.DeleteSupplier)
	api.POST("/suppliers/:id/test-connection", supplierHandler.TestConnection)

	// MODULE H - PURCHASE ORDERS (full implementation)
	// Note: auto-generate must be registered before /:id to avoid Gin capturing it as an id param
	api.POST("/purchase-orders/auto-generate", purchaseOrderHandler.AutoGenerate)
	api.GET("/purchase-orders/due-in", purchaseOrderHandler.GetDueInByProduct)

	// Session 3 Task 4 — Reorder Suggestions
	api.GET("/purchase-orders/suggestions", reorderSuggestionHandler.ListSuggestions)
	api.POST("/purchase-orders/suggestions/generate", reorderSuggestionHandler.GenerateSuggestions)
	api.POST("/purchase-orders/suggestions/:id/approve", reorderSuggestionHandler.ApproveSuggestion)
	api.POST("/purchase-orders/suggestions/:id/dismiss", reorderSuggestionHandler.DismissSuggestion)
	api.GET("/purchase-orders", purchaseOrderHandler.ListPOs)
	api.POST("/purchase-orders", purchaseOrderHandler.CreatePO)
	api.GET("/purchase-orders/:id", purchaseOrderHandler.GetPO)
	api.PUT("/purchase-orders/:id", purchaseOrderHandler.UpdatePO)
	api.POST("/purchase-orders/:id/send", purchaseOrderHandler.SendPO)
	api.POST("/purchase-orders/:id/receive", purchaseOrderHandler.ReceiveGoods)
	api.POST("/purchase-orders/:id/cancel", purchaseOrderHandler.CancelPO)
	api.POST("/purchase-orders/:id/tracking", purchaseOrderHandler.AddTracking)

	// ── Stock Count ────────────────────────────────────────────────────────────
	api.GET("/stock-counts", stockCountHandler.ListCounts)
	api.POST("/stock-counts", stockCountHandler.CreateCount)
	api.GET("/stock-counts/:id", stockCountHandler.GetCount)
	api.POST("/stock-counts/:id/lines", stockCountHandler.UpdateLine)
	api.POST("/stock-counts/:id/commit", stockCountHandler.CommitCount)
	api.POST("/stock-counts/:id/cancel", stockCountHandler.CancelCount)
	api.DELETE("/stock-counts/:id", stockCountHandler.DeleteCount)

	// ── Stock Scrap ────────────────────────────────────────────────────────────
	api.GET("/stock-scraps/stats", stockScrapHandler.GetScrapStats)
	api.GET("/stock-scraps", stockScrapHandler.ListScraps)
	api.POST("/stock-scraps", stockScrapHandler.CreateScrap)

	// ── Forecasting ───────────────────────────────────────────────────────────
	api.GET("/forecasting/settings", forecastingHandler.GetSettings)
	api.PUT("/forecasting/settings", forecastingHandler.UpdateSettings)
	api.GET("/forecasting/dashboard", forecastingHandler.GetDashboard)
	api.GET("/forecasting/products", forecastingHandler.ListProductForecasts)
	api.GET("/forecasting/products/:product_id", forecastingHandler.GetProductForecast)
	api.PUT("/forecasting/products/:product_id", forecastingHandler.UpdateProductForecast)
	api.POST("/forecasting/recalculate", forecastingHandler.Recalculate)

	// ── GAP CLOSURE: FORECASTING EXTENSIONS ───────────────────────────────────
	api.POST("/forecasting/create-po", forecastingHandler.CreatePOFromForecast)
	api.GET("/forecasting/products/:product_id/chart", forecastingHandler.GetForecastChart)

	// ── GAP CLOSURE: BATCHED ITEMS (P1) ───────────────────────────────────────
	api.GET("/products/:id/batches", batchHandler.ListBatches)
	api.POST("/products/:id/batches", batchHandler.CreateBatch)
	api.PUT("/products/:id/batches/:batch_id", batchHandler.UpdateBatch)
	api.DELETE("/products/:id/batches/:batch_id", batchHandler.DeleteBatch)
	api.POST("/products/:id/batches/scan", batchHandler.ScanBatch)

	// ── GAP CLOSURE: PRODUCT EXTENSIONS (P2) ──────────────────────────────────
	api.GET("/products/:id/extended-properties", productExtensionsHandler.GetExtendedProperties)
	api.PUT("/products/:id/extended-properties", productExtensionsHandler.UpdateExtendedProperties)
	api.PUT("/products/:id/identifiers", productExtensionsHandler.UpdateIdentifiers)
	api.GET("/products/:id/stock-history", productExtensionsHandler.GetStockHistory)
	api.GET("/products/:id/stats", productExtensionsHandler.GetStats)
	api.GET("/products/:id/ktypes", productExtensionsHandler.GetKTypes)
	api.PUT("/products/:id/ktypes", productExtensionsHandler.UpdateKTypes)
	api.GET("/products/:id/ai-debug", productExtensionsHandler.GetAIDebug)

	// ── GAP CLOSURE: FBA INBOUND SHIPMENTS (P1) ───────────────────────────────
	api.POST("/fba/shipments", fbaInboundHandler.CreateShipment)
	api.GET("/fba/shipments", fbaInboundHandler.ListShipments)
	api.GET("/fba/shipments/:id", fbaInboundHandler.GetShipment)
	api.PUT("/fba/shipments/:id", fbaInboundHandler.UpdateShipment)
	api.POST("/fba/shipments/:id/plan", fbaInboundHandler.PlanShipment)
	api.POST("/fba/shipments/:id/confirm", fbaInboundHandler.ConfirmShipment)
	api.POST("/fba/shipments/:id/close", fbaInboundHandler.CloseShipment)

	// ── GAP CLOSURE: SYNC STATUS PANEL (P1) ───────────────────────────────────
	api.GET("/sync/status", syncStatusHandler.GetStatus)
	api.GET("/sync/channel-status", syncStatusHandler.GetChannelSyncStatus)
	api.POST("/sync/errors/clear", syncStatusHandler.ClearErrors)

	// ── Session 2 Task 3: Inventory Sync ──────────────────────────────────────
	api.POST("/inventory-sync/trigger", inventorySyncHandler.TriggerSync)
	api.POST("/inventory-sync/trigger-all", inventorySyncHandler.TriggerAll)
	api.GET("/inventory-sync/logs", inventorySyncHandler.GetLogs)

	// ── Session 2 Task 4: Overselling Prevention — Stock Reservations ─────────
	api.POST("/stock-reservations", stockReservationHandler.CreateReservation)
	api.GET("/stock-reservations/:product_id", stockReservationHandler.GetReservationsByProduct)
	api.POST("/stock-reservations/:id/release", stockReservationHandler.ReleaseReservation)
	api.DELETE("/stock-reservations/:id", stockReservationHandler.DeleteReservation)

	// ── GAP CLOSURE: BINRACK / WMS (P2) ───────────────────────────────────────
	api.POST("/locations/:id/binracks", binrackHandler.CreateBinrack)
	api.GET("/locations/:id/binracks", binrackHandler.ListBinracks)
	api.PUT("/binracks/:binrack_id", binrackHandler.UpdateBinrack)
	api.DELETE("/binracks/:binrack_id", binrackHandler.DeleteBinrack)
	api.POST("/stock/move", binrackHandler.MoveStock)
	api.GET("/stock/moves", binrackHandler.ListStockMoves)
	api.GET("/warehouse/replenishment", binrackHandler.GetReplenishment)
	api.GET("/warehouse/zones", binrackHandler.ListZones)
	api.POST("/warehouse/zones", binrackHandler.CreateZone)
	api.PUT("/warehouse/zones/:zone_id", binrackHandler.UpdateZone)
	api.DELETE("/warehouse/zones/:zone_id", binrackHandler.DeleteZone)
	api.GET("/warehouse/binrack/:id/items", binrackHandler.GetBinrackItems)

	// ── GAP CLOSURE: POSTAGE DEFINITIONS (P2) ─────────────────────────────────
	api.GET("/postage-definitions", postageDefinitionHandler.List)
	api.POST("/postage-definitions", postageDefinitionHandler.Create)
	api.POST("/postage-definitions/match", postageDefinitionHandler.Match)
	api.GET("/postage-definitions/:id", postageDefinitionHandler.Get)
	api.PUT("/postage-definitions/:id", postageDefinitionHandler.Update)
	api.DELETE("/postage-definitions/:id", postageDefinitionHandler.Delete)

	// ── GAP CLOSURE: VENDOR ORDERS / AMAZON VENDOR CENTRAL (P3) ──────────────
	api.POST("/vendor-orders/sync", vendorOrderHandler.Sync)
	api.GET("/vendor-orders", vendorOrderHandler.List)
	api.POST("/vendor-orders", vendorOrderHandler.Create)
	api.GET("/vendor-orders/:id", vendorOrderHandler.Get)
	api.POST("/vendor-orders/:id/accept", vendorOrderHandler.Accept)
	api.POST("/vendor-orders/:id/decline", vendorOrderHandler.Decline)

	// ── GAP CLOSURE: SUPPLIER RETURNS (P3) ────────────────────────────────────
	api.GET("/supplier-returns", supplierReturnHandler.List)
	api.POST("/purchase-orders/:id/return", supplierReturnHandler.CreateReturn)

	// ── GAP CLOSURE: PICKLIST (P3) ────────────────────────────────────────────
	api.POST("/orders/picklist", picklistHandler.GeneratePicklist)

	// ── GAP CLOSURE: LABEL PRINTING (P3) ──────────────────────────────────────
	api.GET("/shipments/print-queue", labelPrintingHandler.GetPrintQueue)
	api.POST("/shipments/print", labelPrintingHandler.PrintLabels)

	// ── P0 FEATURES ───────────────────────────────────────────────────────────

	// P0.1 — Listing Descriptions Tab
	api.GET("/products/:id/listing-descriptions", listingDescriptionHandler.List)
	api.PUT("/products/:id/listing-descriptions/:description_id", listingDescriptionHandler.Upsert)
	api.DELETE("/products/:id/listing-descriptions/:description_id", listingDescriptionHandler.Delete)

	// P0.2 — Saved Inventory Views
	api.GET("/inventory-views", inventoryViewHandler.List)
	api.POST("/inventory-views", inventoryViewHandler.Create)
	api.PUT("/inventory-views/:id", inventoryViewHandler.Update)
	api.DELETE("/inventory-views/:id", inventoryViewHandler.Delete)

	// B-007: Saved order views
	api.GET("/order-views", orderViewHandler.List)
	api.POST("/order-views", orderViewHandler.Create)
	api.PUT("/order-views/:id", orderViewHandler.Update)
	api.DELETE("/order-views/:id", orderViewHandler.Delete)

	// B-002: Manual order creation
	api.POST("/orders/manual", orderManagementHandler.CreateManualOrder)
	// B-003: Order editing
	api.PATCH("/orders/:id/edit", orderManagementHandler.UpdateOrder)
	// B-004: Order merge
	api.POST("/orders/merge", orderManagementHandler.MergeOrders)
	// B-005: Order split
	api.POST("/orders/:id/split", orderManagementHandler.SplitOrder)
	// B-006: Order cancellation with reason codes
	api.POST("/orders/:id/cancel", orderManagementHandler.CancelOrder)

	// P0.3 — Automation Logs
	api.GET("/automation-logs", automationLogHandler.List)
	api.POST("/automation-logs/clear", automationLogHandler.Clear)

	// Session 5 — Automation Log retry
	api.POST("/automation-logs/:id/retry", automationLogHandler.Retry)

	// Session 5 — Security Settings
	api.GET("/security-settings", securitySettingsHandler.GetSecuritySettings)
	api.PUT("/security-settings", securitySettingsHandler.UpdateSecuritySettings)
	api.POST("/admin/data-purge", securitySettingsHandler.DataPurge)
	api.POST("/admin/obfuscate-customers", securitySettingsHandler.ObfuscateAllCustomers)
	api.POST("/admin/system-reset", securitySettingsHandler.SystemReset)
	api.POST("/admin/purge-extended-data", securitySettingsHandler.PurgeExtendedData)

	// Session 6 — User Audit Log
	api.GET("/user-audit-log", userAuditHandler.ListUserAuditLog)

	// Session 7 — App Store
	api.GET("/apps", appStoreHandler.ListApps)
	api.GET("/apps/installed", appStoreHandler.ListInstalledApps)
	api.POST("/apps/:id/install", appStoreHandler.InstallApp)
	api.DELETE("/apps/:id/uninstall", appStoreHandler.UninstallApp)
	api.POST("/apps/seed", appStoreHandler.SeedApps)

	// P0.5 — WMS Product Tab — Storage Groups
	api.GET("/storage-groups", storageGroupHandler.List)
	api.POST("/storage-groups", storageGroupHandler.Create)
	api.PUT("/storage-groups/:id", storageGroupHandler.Update)
	api.DELETE("/storage-groups/:id", storageGroupHandler.Delete)

	// ── P1.1  Per-location forecast ────────────────────────────────────────────
	api.GET("/forecasting/products/:product_id/by-location", forecastingHandler.GetForecastByLocation)

	// ── P1.7  Product audit trail ──────────────────────────────────────────────
	api.GET("/products/:id/audit", warehouseLocationHandler.GetProductAuditTrail)

	// ── P1.8  Price sync ───────────────────────────────────────────────────────
	api.GET("/price-sync/rules", priceSyncHandler.ListRules)
	api.POST("/price-sync/rules", priceSyncHandler.CreateRule)
	api.PUT("/price-sync/rules/:id", priceSyncHandler.UpdateRule)
	api.DELETE("/price-sync/rules/:id", priceSyncHandler.DeleteRule)
	api.POST("/price-sync/trigger", priceSyncHandler.TriggerSync)
	api.GET("/price-sync/log", priceSyncHandler.GetLog)

	// P0.5 — WMS Product Tab — per-product WMS config
	api.GET("/products/:id/wms-config", productExtensionsHandler.GetWMSConfig)
	api.PUT("/products/:id/wms-config", productExtensionsHandler.UpdateWMSConfig)

	// RMAs & RETURNS
	api.GET("/rmas", rmaHandler.ListRMAs)
	api.POST("/rmas", rmaHandler.CreateRMA)
	api.GET("/rmas/config", rmaHandler.GetConfig)
	api.POST("/rmas/config", rmaHandler.UpdateConfig)
	api.POST("/rmas/sync", rmaHandler.SyncRMAs)
	api.GET("/rmas/:id", rmaHandler.GetRMA)
	api.PUT("/rmas/:id", rmaHandler.UpdateRMA)

	// ── Session 2 Task 5: Refund Downloads ───────────────────────────────────
	api.GET("/refund-downloads", refundDownloadHandler.ListRefundDownloads)
	api.POST("/refund-downloads/:id/match-rma", refundDownloadHandler.MatchToRMA)
	api.POST("/amazon/orders/:id/refunds", refundDownloadHandler.DownloadAmazonRefunds)
	api.POST("/ebay/orders/:id/refunds", refundDownloadHandler.DownloadEbayRefunds)
	api.POST("/shopify/orders/:id/refunds", refundDownloadHandler.DownloadShopifyRefunds)
	api.POST("/rmas/:id/authorise", rmaHandler.AuthoriseRMA)
	api.POST("/rmas/:id/receive", rmaHandler.ReceiveRMA)
	api.POST("/rmas/:id/inspect", rmaHandler.InspectRMA)
	api.POST("/rmas/:id/restock/:line_id", rmaHandler.RestockLine)
	api.POST("/rmas/:id/resolve", rmaHandler.ResolveRMA)
	api.POST("/rmas/:id/exchange", rmaHandler.ExchangeRMA)
	api.POST("/rmas/:id/push-refund", refundPushHandler.PushRefund)

	// BUYER MESSAGES / HELPDESK
	api.GET("/messages", messagingHandler.ListConversations)
	api.GET("/messages/unread-count", messagingHandler.UnreadCount)
	api.GET("/messages/canned", messagingHandler.ListCannedResponses)
	api.POST("/messages/canned", messagingHandler.CreateCannedResponse)
	api.PUT("/messages/canned/:id", messagingHandler.UpdateCannedResponse)
	api.DELETE("/messages/canned/:id", messagingHandler.DeleteCannedResponse)
	api.POST("/messages/sync", messagingHandler.Sync)
	api.POST("/messages/:id/assign", messagingHandler.Assign)
	api.GET("/messages/members", messagingHandler.ListMembers)
	api.PUT("/messages/notif-prefs", messagingHandler.UpdateNotifPrefs)
	api.GET("/webhook-health", orderWebhookHandler.GetWebhookHealthStatus)
	api.POST("/messages/:id/ai-process", messagingAIHandler.ProcessConversation)
	api.DELETE("/messages/:id/drafts/:draft_id", messagingHandler.DeleteDraft)
	api.GET("/messages/ai-audit", messagingAIHandler.GetAuditLog)
	api.GET("/settings/messaging-ai", messagingAIHandler.GetMessagingAISettings)
	api.PUT("/settings/messaging-ai", messagingAIHandler.UpdateMessagingAISettings)
	api.POST("/messages", messagingHandler.CreateConversation)
	api.GET("/messages/:id", messagingHandler.GetConversation)
	api.POST("/messages/:id/reply", messagingHandler.Reply)
	api.POST("/messages/:id/resolve", messagingHandler.Resolve)
	api.POST("/messages/:id/read", messagingHandler.MarkRead)

	// ── MODULE K — AUTH (public — no tenant middleware) ─────────────────────
	// These sit outside the api group so they don't require X-Tenant-Id
	router.POST("/api/v1/auth/register", authHandler.Register)
	router.POST("/api/v1/auth/me", authHandler.MeByFirebaseUID)
	router.GET("/api/v1/auth/invite/:token", authHandler.GetInvitation)
	router.POST("/api/v1/auth/invite/accept", authHandler.AcceptInvitation)

	// ── MODULE K — USER & TEAM (tenant-scoped) ──────────────────────────────
	api.GET("/users/members", userHandler.ListMembers)
	api.PUT("/users/members/:membership_id/role", userHandler.ChangeRole)
	api.PUT("/users/members/:membership_id/permissions", userHandler.UpdatePermissions)
	api.DELETE("/users/members/:membership_id", userHandler.RemoveMember)
	api.POST("/users/invite", authHandler.InviteUser)
	api.GET("/users/invitations", userHandler.ListInvitations)
	api.DELETE("/users/invitations/:token", userHandler.RevokeInvitation)
	api.GET("/users/:membershipId/security-info", securitySettingsHandler.GetMemberSecurityInfo)
	api.POST("/users/members/:membership_id/send-password-reset", userHandler.SendPasswordReset)
	api.PATCH("/users/profile", userHandler.UpdateProfile)

	// ── MODULE K — BILLING ───────────────────────────────────────────────────
	router.GET("/api/v1/billing/plans", billingHandler.GetPlans) // public — used on signup
	api.GET("/billing/status", billingHandler.GetBillingStatus)
	api.GET("/billing/usage", billingHandler.GetUsageSummary)
	api.GET("/billing/audit", billingHandler.GetAuditLog)
	api.POST("/billing/paypal/subscribe", billingHandler.CreatePayPalSubscription)
	api.POST("/billing/stripe/checkout", billingHandler.CreateStripeCheckoutSession)
	api.POST("/billing/stripe/portal", billingHandler.CreateStripePortalSession)

	// PayPal webhook — no tenant middleware, verified by signature
	router.POST("/webhooks/paypal", billingHandler.PayPalWebhook)
	router.POST("/webhooks/stripe", billingHandler.StripeWebhook)  // Stripe webhook — no tenant middleware, verified by signature

	// Session 3 Task 5 — Carrier tracking webhooks (public, no tenant middleware, verified by carrier signature)
	router.POST("/webhooks/tracking/royal-mail", trackingWebhookHandler.RoyalMailWebhook)
	router.POST("/webhooks/tracking/dpd", trackingWebhookHandler.DPDWebhook)
	router.POST("/webhooks/tracking/evri", trackingWebhookHandler.EvriWebhook)

	// Session 3 Task 6 — Public returns portal (no tenant middleware — tenant identified by path param)
	router.GET("/api/v1/public/returns/config/:tenant_id", returnsPortalHandler.GetConfig)
	router.POST("/api/v1/public/returns/lookup", returnsPortalHandler.LookupOrder)
	router.POST("/api/v1/public/returns/submit", returnsPortalHandler.SubmitReturn)
	router.GET("/api/v1/public/returns/rma/:rma_number", returnsPortalHandler.GetRMAStatus)

	// ── MODULE L — PAGEBUILDER TEMPLATES ────────────────────────────────────
	api.GET("/templates", templateHandler.ListTemplates)
	api.POST("/templates", templateHandler.CreateTemplate)
	api.POST("/templates/ai/generate-text", templateHandler.GenerateText)
	api.POST("/templates/upload-image", templateHandler.UploadImage)
	api.GET("/templates/default/:type", templateHandler.GetDefault)
	api.GET("/templates/:id", templateHandler.GetTemplate)
	api.PUT("/templates/:id", templateHandler.UpdateTemplate)
	api.DELETE("/templates/:id", templateHandler.DeleteTemplate)
	api.POST("/templates/:id/default", templateHandler.SetDefault)
	api.PATCH("/templates/:id/toggle", templateHandler.ToggleTemplate)
	api.POST("/templates/:id/render", templateHandler.RenderTemplate)
	api.POST("/templates/:id/send", templateHandler.SendEmail)

	// ── SESSION 1: CONFIGURATOR SYSTEM ──────────────────────────────────────
	// Note: auto-select must be registered BEFORE /:id to avoid route conflict
	api.POST("/configurators/auto-select", configuratorHandler.AutoSelect)
	api.POST("/configurators/ai-setup", configuratorAIHandler.AISetup) // USP-01 — must be before /:id
	api.GET("/configurators", configuratorHandler.ListConfigurators)
	api.POST("/configurators", configuratorHandler.CreateConfigurator)
	api.GET("/configurators/:id", configuratorHandler.GetConfigurator)
	api.PUT("/configurators/:id", configuratorHandler.UpdateConfigurator)
	api.DELETE("/configurators/:id", configuratorHandler.DeleteConfigurator)
	api.POST("/configurators/:id/duplicate", configuratorHandler.DuplicateConfigurator)
	api.POST("/configurators/:id/revise", configuratorHandler.ReviseConfigurator)
	api.POST("/configurators/:id/assign-listings", configuratorHandler.AssignListings)
	api.POST("/configurators/:id/remove-listings", configuratorHandler.RemoveListings)

	// Seller profile (tenant settings)
	api.GET("/settings/seller", templateHandler.GetSellerProfile)
	api.PUT("/settings/seller", templateHandler.UpdateSellerProfile)

	// Extended Settings (Email, Notifications, Currency)
	api.GET("/settings/email", settingsHandler.GetEmailSettings)
	api.PUT("/settings/email", settingsHandler.UpdateEmailSettings)
	api.POST("/settings/email/test", settingsHandler.TestEmailSettings)
	api.GET("/settings/notifications", settingsHandler.GetNotificationSettings)
	api.PUT("/settings/notifications", settingsHandler.UpdateNotificationSettings)
	api.GET("/cancellation-alerts", cancellationAlertHandler.ListAlerts)
	api.POST("/cancellation-alerts/:id/acknowledge", cancellationAlertHandler.AcknowledgeAlert)
	api.GET("/settings/currency", settingsHandler.GetCurrencyRates)
	api.POST("/settings/currency", settingsHandler.AddCurrencyRate)
	api.DELETE("/settings/currency/:id", settingsHandler.DeleteCurrencyRate)

	// Task 6: Order tag definitions (CRUD in settings)
	api.GET("/settings/order-tags", orderActionsHandler.ListTagDefinitions)
	api.POST("/settings/order-tags", orderActionsHandler.CreateTagDefinition)
	api.PUT("/settings/order-tags/:id", orderActionsHandler.UpdateTagDefinition)
	api.DELETE("/settings/order-tags/:id", orderActionsHandler.DeleteTagDefinition)

	// Task 14: Tax / VAT settings
	api.GET("/settings/tax", settingsHandler.GetTaxSettings)
	api.PUT("/settings/tax", settingsHandler.UpdateTaxSettings)

	// ── ORDER & PRINT SETTINGS (Session 3) ───────────────────────────────────
	api.GET("/settings/orders", settingsHandler.GetOrderSettings)
	api.PUT("/settings/orders", settingsHandler.UpdateOrderSettings)
	api.GET("/settings/print", settingsHandler.GetPrintSettings)
	api.PUT("/settings/print", settingsHandler.UpdatePrintSettings)

	// ── WMS SETTINGS (Session 4) ─────────────────────────────────────────────
	api.GET("/settings/wms", settingsHandler.GetWMSSettings)
	api.PUT("/settings/wms", settingsHandler.UpdateWMSSettings)

	// ── FEATURE MODULES ───────────────────────────────────────────────────
	api.GET("/settings/modules", settingsHandler.GetModules)
	api.PUT("/settings/modules", settingsHandler.UpdateModules)

	// ── SETUP WIZARD ──────────────────────────────────────────────────────
	api.GET("/settings/setup-status", settingsHandler.GetSetupStatus)
	api.POST("/settings/setup-complete", settingsHandler.CompleteSetup)
	api.GET("/settings/selected-channels", settingsHandler.GetSelectedChannels)

	// ── COUNTRIES & TAX RATES (Session 2) ────────────────────────────────────
	api.GET("/settings/countries", settingsHandler.ListCountries)
	api.POST("/settings/countries", settingsHandler.CreateCountry)
	api.PUT("/settings/countries/:id", settingsHandler.UpdateCountry)
	api.DELETE("/settings/countries/:id", settingsHandler.DeleteCountry)

	// ── BIN TYPES (Session 4) ────────────────────────────────────────────────
	api.GET("/settings/bin-types", binTypeHandler.ListBinTypes)
	api.POST("/settings/bin-types", binTypeHandler.CreateBinType)
	api.PUT("/settings/bin-types/:id", binTypeHandler.UpdateBinType)
	api.DELETE("/settings/bin-types/:id", binTypeHandler.DeleteBinType)

	// ── SCHEDULES (Session 4) ────────────────────────────────────────────────
	api.GET("/schedules", scheduleHandler.ListSchedules)
	api.POST("/schedules", scheduleHandler.CreateSchedule)
	api.PUT("/schedules/:id", scheduleHandler.UpdateSchedule)
	api.DELETE("/schedules/:id", scheduleHandler.DeleteSchedule)

	// ── TEMU WIZARD ─────────────────────────────────────────────────────────
	if temuWizardHandler != nil {
		api.GET("/temu-wizard/status", temuWizardHandler.GetStatus)
		api.PUT("/temu-wizard/status", temuWizardHandler.UpdateStatus)
		api.POST("/temu-wizard/generate-xlsx", temuWizardHandler.GenerateXLSX)
		api.GET("/temu-wizard/download-xlsx", temuWizardHandler.DownloadXLSX)
		api.POST("/temu-wizard/upload-xlsx", temuWizardHandler.UploadXLSX)
		api.POST("/temu-wizard/generate-listings", temuWizardHandler.GenerateListings)
		api.POST("/temu-wizard/generate-listings-async", temuWizardHandler.GenerateListingsAsync)
		api.GET("/temu-wizard/generation-job/:job_id", temuWizardHandler.GetGenerationJob)
		api.GET("/temu-wizard/all-drafts", temuWizardHandler.GetAllDrafts)
		api.GET("/temu/drafts/stats", temuWizardHandler.GetDraftStats)
	}

	// Admin-only endpoints (no tenant scope — internal use)
	router.PUT("/api/v1/admin/tenants/:tenant_id/plan-override", billingHandler.SetPlanOverride)
	router.GET("/api/v1/admin/tenants/:tenant_id/plan-override", billingHandler.GetPlanOverride)
	router.GET("/api/v1/admin/credit-rates", billingHandler.GetCreditRates)
	router.PUT("/api/v1/admin/credit-rates", billingHandler.UpdateCreditRates)
	// Marketplace registry admin — enable/disable channels, set images, sort order
	router.PUT("/api/v1/admin/marketplace/:id", marketplaceHandler.AdminUpsertMarketplace)
	router.GET("/api/v1/admin/marketplace", marketplaceHandler.GetRegistry) // admin view (same data)

	// Ops console — cross-tenant job observability (no tenant middleware — admin only)
	router.GET("/api/v1/admin/ops/jobs", opsHandler.ListAllJobs)
	router.GET("/api/v1/admin/ops/jobs/:tenant_id/:collection/:job_id", opsHandler.GetJobDetail)
	router.POST("/api/v1/admin/ops/jobs/:tenant_id/:collection/:job_id/cancel", opsHandler.CancelJob)
	router.POST("/api/v1/admin/ops/jobs/:tenant_id/:collection/:job_id/retry", opsHandler.RetryJob)
	router.DELETE("/api/v1/admin/ops/jobs/:tenant_id/:collection/:job_id", opsHandler.DeleteJob)
	router.POST("/api/v1/admin/ops/copy-products", opsHandler.CopyProducts)

	// H-001: In-app changelog
	api.GET("/changelog", changelogHandler.ListEntries)
	api.POST("/changelog", changelogHandler.CreateEntry)
	api.POST("/changelog/seen", changelogHandler.MarkSeen)

	// D-001: Analytics dashboard
	api.GET("/analytics/overview", analyticsHandler.GetOverview)
	api.GET("/analytics/orders", analyticsHandler.GetOrdersAnalytics)
	api.GET("/analytics/revenue", analyticsHandler.GetRevenueAnalytics)
	api.GET("/analytics/top-products", analyticsHandler.GetTopProducts)
	api.GET("/analytics/inventory", analyticsHandler.GetInventoryHealth)
	api.GET("/analytics/returns", analyticsHandler.GetReturnsAnalytics)
	// S1: Home dashboard & stock consumption
	api.GET("/analytics/home", analyticsHandler.GetHome)
	api.GET("/analytics/stock-consumption", analyticsHandler.GetStockConsumption)
	// S2: Stock dashboards & pivotal analytics
	api.GET("/analytics/inventory-dashboard", analyticsHandler.GetInventoryDashboard)
	api.GET("/analytics/order-dashboard", analyticsHandler.GetOrderDashboard)
	api.GET("/analytics/pivot/fields", analyticsHandler.GetPivotFields)
	api.POST("/analytics/pivot", analyticsHandler.RunPivot)

	// Session 8: Advanced analytics
	api.GET("/analytics/channel-pnl", analyticsHandler.ChannelPnL)
	api.GET("/analytics/listing-health", analyticsHandler.ListingHealthBulk)
	api.GET("/analytics/reconciliation-health", analyticsHandler.ReconciliationHealth)

	// Session 4: Reporting & analytics
	api.GET("/analytics/reports/orders-by-channel", analyticsHandler.GetOrdersByChannel)
	api.GET("/analytics/reports/orders-by-date", analyticsHandler.GetOrdersByDate)
	api.GET("/analytics/reports/orders-by-product", analyticsHandler.GetOrdersByProduct)
	api.GET("/analytics/reports/despatch-performance", analyticsHandler.GetDespatchPerformance)
	api.GET("/analytics/reports/returns", analyticsHandler.GetReturnsReport)
	api.GET("/analytics/reports/financial", analyticsHandler.GetFinancialReport)
	api.GET("/analytics/operational", analyticsHandler.GetOperationalDashboard)
	api.GET("/analytics/operational/channel-sync", analyticsHandler.GetChannelSyncHealth)
	api.GET("/analytics/operational/throughput", analyticsHandler.GetWarehouseThroughput)
	api.GET("/analytics/reports/export", analyticsHandler.ExportReportCSV)
	api.GET("/products/:id/health-score", analyticsHandler.GetProductHealthScore)

	// Session 8: Channel demand signals + reorder alerts
	api.GET("/forecasting/channel-demand", forecastingHandler.GetChannelDemand)
	api.POST("/forecasting/reorder-alerts/:product_id/snooze", forecastingHandler.SnoozeReorderAlert)

	// Session 9: Automated reorder trigger
	api.POST("/forecasting/auto-reorder/run", autoReorderHandler.RunCheck)
	api.GET("/forecasting/auto-reorder/log", autoReorderHandler.GetLog)
	api.GET("/forecasting/auto-reorder/settings", autoReorderHandler.GetSettings)
	api.PUT("/forecasting/auto-reorder/settings", autoReorderHandler.UpdateSettings)

	// Session 8: Warehouse allocation rules
	api.GET("/warehouses/allocation-rules", warehouseLocationHandler.ListAllocationRules)
	api.POST("/warehouses/allocation-rules", warehouseLocationHandler.CreateAllocationRule)
	api.PUT("/warehouses/allocation-rules/:id", warehouseLocationHandler.UpdateAllocationRule)
	api.DELETE("/warehouses/allocation-rules/:id", warehouseLocationHandler.DeleteAllocationRule)

	// D-002: Report builder
	api.POST("/reports/run", reportHandler.RunReport)
	api.GET("/reports/saved", reportHandler.ListSavedReports)
	api.POST("/reports/saved", reportHandler.SaveReport)
	api.GET("/reports/fields/:entity", reportHandler.GetEntityFields)

	// ── SESSION 4 — BACK MARKET ───────────────────────────────────────────────
	backmarketGroup := api.Group("/backmarket")
	{
		backmarketGroup.POST("/orders/import", backmarketOrdersHandler.TriggerImport)
		backmarketGroup.GET("/orders", backmarketOrdersHandler.ListOrders)
		backmarketGroup.POST("/orders/:id/ship", backmarketOrdersHandler.PushTracking)
		// Session 6.2 — Bulk ops
		backmarketGroup.POST("/orders/bulk/ship", backmarketBulkHandler.BulkShip)
		backmarketGroup.GET("/orders/bulk/export", backmarketBulkHandler.BulkExport)
	}

	// ── SESSION 4 — ZALANDO ───────────────────────────────────────────────────
	zalandoGroup := api.Group("/zalando")
	{
		zalandoGroup.POST("/orders/import", zalandoOrdersHandler.TriggerImport)
		zalandoGroup.GET("/orders", zalandoOrdersHandler.ListOrders)
		zalandoGroup.POST("/orders/:id/ship", zalandoOrdersHandler.PushTracking)
		// Session 6.2 — Bulk ops
		zalandoGroup.POST("/orders/bulk/ship", zalandoBulkHandler.BulkShip)
		zalandoGroup.GET("/orders/bulk/export", zalandoBulkHandler.BulkExport)
	}

	// ── SESSION 4 — BOL.COM ───────────────────────────────────────────────────
	bolGroup := api.Group("/bol")
	{
		bolGroup.POST("/orders/import", bolOrdersHandler.TriggerImport)
		bolGroup.GET("/orders", bolOrdersHandler.ListOrders)
		bolGroup.POST("/orders/:id/ship", bolOrdersHandler.PushTracking)
		// Session 6.2 — Bulk ops
		bolGroup.POST("/orders/bulk/ship", bolBulkHandler.BulkShip)
		bolGroup.GET("/orders/bulk/export", bolBulkHandler.BulkExport)
	}

	// ── SESSION 4 — LAZADA ────────────────────────────────────────────────────
	lazadaGroup := api.Group("/lazada")
	{
		lazadaGroup.POST("/orders/import", lazadaOrdersHandler.TriggerImport)
		lazadaGroup.GET("/orders", lazadaOrdersHandler.ListOrders)
		lazadaGroup.POST("/orders/:id/ship", lazadaOrdersHandler.PushTracking)
		// Session 6.2 — Bulk ops
		lazadaGroup.POST("/orders/bulk/ship", lazadaBulkHandler.BulkShip)
		lazadaGroup.GET("/orders/bulk/export", lazadaBulkHandler.BulkExport)
	}

	// ── SESSION 6.2 — PACKING SLIP (cross-channel) ────────────────────────────
	api.POST("/orders/packing-slip", packingSlipHandler.GeneratePackingSlip)

	// ── SESSION 4 — SKU RECONCILIATION ────────────────────────────────────────
	api.POST("/marketplace/credentials/:id/auto-link", reconcileHandler.RunAutoLink)
	api.GET("/marketplace/credentials/:id/reconcile", reconcileHandler.GetReconcileState)
	api.POST("/marketplace/credentials/:id/reconcile/confirm", reconcileHandler.ConfirmReconcile)
	api.GET("/marketplace/credentials/:id/reconcile/export", reconcileHandler.ExportUnmatched)
	api.POST("/marketplace/credentials/:id/reconcile/import", reconcileHandler.ImportResolutions)
	api.GET("/marketplace/credentials/:id/reconcile/history", reconcileHandler.GetHistory)

	// ── SESSION 1 — NAVIGATION & GLOBAL UI ───────────────────────────────────

	// Task 2: System Notifications
	api.GET("/notifications", notificationHandler.GetNotifications)
	api.POST("/notifications/mark-read", notificationHandler.MarkNotificationsRead)

	// Task 3: User Profile
	api.GET("/user/profile", userHandler.GetProfile)
	api.PUT("/user/profile", userHandler.PutProfile)
	api.POST("/user/phone/send-otp", userHandler.SendPhoneOTP)
	api.POST("/user/phone/verify-otp", userHandler.VerifyPhoneOTP)
	api.PUT("/user/notif-prefs", userHandler.UpdateNotifPrefs)

	// Task 4: Email Templates & Logs
	api.GET("/email-templates", emailTemplateHandler.ListEmailTemplates)
	api.POST("/email-templates", emailTemplateHandler.CreateEmailTemplate)
	api.GET("/email-templates/:id", emailTemplateHandler.GetEmailTemplate)
	api.PUT("/email-templates/:id", emailTemplateHandler.UpdateEmailTemplate)
	api.DELETE("/email-templates/:id", emailTemplateHandler.DeleteEmailTemplate)
	api.GET("/email-logs", emailLogHandler.ListEmailLogs)
	api.GET("/sent-mail", sentMailHandler.ListSentMail)

	// Task 5: Pickwaves / Batch Picking
	api.POST("/pickwaves", pickwaveHandler.CreatePickwave)
	api.GET("/pickwaves", pickwaveHandler.ListPickwaves)
	api.GET("/pickwaves/:id", pickwaveHandler.GetPickwave)
	api.PUT("/pickwaves/:id", pickwaveHandler.UpdatePickwave)
	api.DELETE("/pickwaves/:id", pickwaveHandler.DeletePickwave)
	api.PUT("/pickwaves/:id/lines/:line_id", pickwaveHandler.UpdatePickwaveLine)
	api.PUT("/pickwaves/:id/status", pickwaveHandler.UpdatePickwaveStatus)

	// ── KEYWORD INTELLIGENCE & SEO OPTIMISATION (Session 1) ─────────────────
	api.POST("/products/:id/keyword-intelligence/ingest", keywordIntelligenceHandler.Ingest)
	api.POST("/products/:id/keyword-intelligence/refresh", keywordIntelligenceHandler.Refresh)
	api.GET("/products/:id/keyword-intelligence", keywordIntelligenceHandler.GetKeywordIntelligence)
	api.GET("/listings/:id/seo-score", keywordIntelligenceHandler.GetSEOScore)
	api.GET("/listings/seo-summary", keywordIntelligenceHandler.GetSEOSummary)

	// ── SESSION 7: BULK OPTIMISE ──────────────────────────────────────────────
	api.POST("/listings/bulk-optimise", bulkOptimiseHandler.BulkOptimise)

	// ── SESSION 7: ADMIN PLATFORM COST ───────────────────────────────────────
	router.GET("/api/v1/admin/platform-cost-summary", adminHandler.PlatformCostSummary)

	return router
}
