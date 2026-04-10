// ============================================================================
// MARKETPLACE ADAPTER TYPES
// ============================================================================

export interface ProductFormData {
  title: string;
  brand: string;
  description: string;
  keyFeatures: string[];
  categories: string[];
  tags: string;
  price?: number;
  compareAtPrice?: number;
  images: string[];
  dimensions?: {
    width: number;
    height: number;
    length: number;
    unit: string;
  };
  weight?: {
    value: number;
    unit: string;
  };
  attributes?: Record<string, string>;
}

export interface ValidationResult {
  isValid: boolean;
  errors: Array<{ field: string; message: string }>;
}

export interface MarketplaceAdapter {
  id: string;
  name: string;
  platform: string;
  icon: string;
  color: string;
  bgColor: string;
  isConnected: boolean;
  
  // Render the marketplace-specific form
  FormComponent: React.ComponentType<MarketplaceFormProps>;
  
  // Transform core data to marketplace format
  syncFromCore: (coreData: ProductFormData) => any;
  
  // Validate marketplace-specific requirements
  validate: (data: any) => ValidationResult;
}

export interface MarketplaceFormProps {
  coreData: ProductFormData;
  marketplaceData: any;
  onChange: (data: any) => void;
  onSync: () => void;
}

export interface MarketplaceData {
  [marketplaceId: string]: any;
}
