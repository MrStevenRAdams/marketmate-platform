import React, { useState } from 'react';
import { ProductFormData, MarketplaceData } from './types';
import { getConnectedMarketplaces, getMarketplaceById } from './MarketplaceRegistry';

interface Props {
  coreData: ProductFormData;
  marketplaceData: MarketplaceData;
  onChange: (data: MarketplaceData) => void;
  onShowBasicDetails: () => void;
  onHideBasicDetails: () => void;
  showingBasicDetails: boolean;
}

export default function MarketplaceSidebar({ 
  coreData, 
  marketplaceData, 
  onChange,
  onShowBasicDetails,
  onHideBasicDetails,
  showingBasicDetails 
}: Props) {
  const [selectedMarketplace, setSelectedMarketplace] = useState<string | null>(null);
  const connectedMarketplaces = getConnectedMarketplaces();
  const currentMarketplace = selectedMarketplace ? getMarketplaceById(selectedMarketplace) : null;

  const handleMarketplaceSelect = (marketplaceId: string) => {
    setSelectedMarketplace(marketplaceId);
    onHideBasicDetails(); // Hide basic details form
  };

  const handleBackToList = () => {
    setSelectedMarketplace(null);
    onShowBasicDetails(); // Show basic details when going back to list
  };

  const handleMarketplaceDataChange = (data: any) => {
    if (!selectedMarketplace) return;
    onChange({
      ...marketplaceData,
      [selectedMarketplace]: data,
    });
  };

  const handleSync = () => {
    if (!currentMarketplace) return;
    
    // Sync core data to marketplace format
    const syncedData = currentMarketplace.syncFromCore(coreData);
    handleMarketplaceDataChange({
      ...marketplaceData[selectedMarketplace!],
      ...syncedData,
    });
  };

  // ============================================================================
  // RENDER: MARKETPLACE LIST (Fixed Sidebar)
  // ============================================================================
  if (!selectedMarketplace) {
    return (
      <div 
        className="card" 
        style={{ 
          position: 'sticky',
          top: '20px',
          maxHeight: 'calc(100vh - 40px)',
          overflow: 'auto'
        }}
      >
        <div className="card-header">
          <h2 style={{ fontSize: '16px', fontWeight: 600 }}>Forms</h2>
          <p style={{ fontSize: '13px', color: 'var(--text-muted)', marginTop: '4px' }}>
            Switch between core and marketplace forms
          </p>
        </div>
        <div style={{ padding: 'var(--spacing-lg)' }}>
          <div className="space-y-2">
            {/* Basic Details Option */}
            <div
              className={`p-3 border rounded-lg transition-all cursor-pointer ${
                showingBasicDetails
                  ? 'border-[var(--primary)] bg-[var(--primary)]/10'
                  : 'border-[var(--border)] hover:border-[var(--primary)] hover:bg-[var(--bg-tertiary)]'
              }`}
              onClick={onShowBasicDetails}
            >
              <div className="flex items-center gap-3">
                <div className={`w-10 h-10 rounded-full flex items-center justify-center ${
                  showingBasicDetails ? 'bg-[var(--primary)]' : 'bg-gray-100'
                }`}>
                  <i className={`ri-file-list-3-line text-xl ${
                    showingBasicDetails ? 'text-white' : 'text-gray-600'
                  }`}></i>
                </div>
                <div className="flex-1 min-w-0">
                  <div className="text-sm font-medium text-[var(--text-primary)]">
                    Basic Details
                  </div>
                  <div className="text-xs text-[var(--text-muted)]">
                    Core product information
                  </div>
                </div>
                {showingBasicDetails && (
                  <i className="ri-check-line text-[var(--primary)] text-xl"></i>
                )}
              </div>
            </div>

            {/* Divider */}
            <div className="relative py-2">
              <div className="absolute inset-0 flex items-center">
                <div className="w-full border-t border-[var(--border)]"></div>
              </div>
              <div className="relative flex justify-center text-xs">
                <span className="bg-[var(--bg-primary)] px-2 text-[var(--text-muted)]">
                  Marketplaces
                </span>
              </div>
            </div>

            {/* Marketplace Options */}
            {connectedMarketplaces.map((marketplace) => (
              <div
                key={marketplace.id}
                className="p-3 border border-[var(--border)] rounded-lg hover:border-[var(--primary)] hover:bg-[var(--bg-tertiary)] transition-all cursor-pointer group"
                onClick={() => handleMarketplaceSelect(marketplace.id)}
              >
                <div className="flex items-center gap-3">
                  <div className={`w-10 h-10 rounded-full flex items-center justify-center ${marketplace.bgColor}`}>
                    <i className={`${marketplace.icon} text-xl ${marketplace.color}`}></i>
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="text-sm font-medium text-[var(--text-primary)] truncate">
                      {marketplace.name}
                    </div>
                    <div className="text-xs text-[var(--text-muted)]">
                      {marketplace.platform}
                    </div>
                  </div>
                  <i className="ri-arrow-right-s-line text-[var(--text-muted)] group-hover:text-[var(--primary)] transition-colors"></i>
                </div>

                {/* Status Indicator */}
                {marketplaceData[marketplace.id] && (
                  <div className="mt-2 pt-2 border-t border-[var(--border)]">
                    <div className="flex items-center gap-2 text-xs">
                      <div className="w-2 h-2 rounded-full bg-[var(--success)]"></div>
                      <span className="text-[var(--text-muted)]">Data configured</span>
                    </div>
                  </div>
                )}
              </div>
            ))}

            {connectedMarketplaces.length === 0 && (
              <div className="text-center py-8">
                <i className="ri-store-line text-4xl text-[var(--text-muted)] mb-3"></i>
                <p className="text-[var(--text-muted)] text-sm">
                  No marketplaces connected
                </p>
                <button className="btn btn-primary btn-sm mt-3">
                  <i className="ri-add-line mr-2"></i>
                  Connect Marketplace
                </button>
              </div>
            )}
          </div>
        </div>
      </div>
    );
  }

  // ============================================================================
  // RENDER: MARKETPLACE FORM (Full Page)
  // ============================================================================
  if (!currentMarketplace) return null;

  const FormComponent = currentMarketplace.FormComponent;
  const data = marketplaceData[selectedMarketplace] || {};

  return (
    <div 
      className="card" 
      style={{ 
        position: 'sticky',
        top: '20px',
        maxHeight: 'calc(100vh - 40px)',
        display: 'flex',
        flexDirection: 'column'
      }}
    >
      {/* Header */}
      <div className="card-header" style={{ borderBottom: '1px solid var(--border)', flexShrink: 0 }}>
        <button
          onClick={handleBackToList}
          className="flex items-center gap-2 text-[var(--text-muted)] hover:text-[var(--text-primary)] transition-colors cursor-pointer mb-2"
        >
          <i className="ri-arrow-left-line"></i>
          <span className="text-sm">Back to forms</span>
        </button>
        <div className="flex items-center gap-3">
          <div className={`w-10 h-10 rounded-full flex items-center justify-center ${currentMarketplace.bgColor}`}>
            <i className={`${currentMarketplace.icon} text-xl ${currentMarketplace.color}`}></i>
          </div>
          <div>
            <h2 style={{ fontSize: '16px', fontWeight: 600 }}>
              {currentMarketplace.name}
            </h2>
            <p style={{ fontSize: '13px', color: 'var(--text-muted)' }}>
              {currentMarketplace.platform}
            </p>
          </div>
        </div>
      </div>

      {/* Form Content - Scrollable */}
      <div style={{ 
        padding: 'var(--spacing-lg)', 
        overflowY: 'auto',
        flexGrow: 1
      }}>
        <FormComponent
          coreData={coreData}
          marketplaceData={data}
          onChange={handleMarketplaceDataChange}
          onSync={handleSync}
        />
      </div>

      {/* Validation Errors */}
      {(() => {
        const validation = currentMarketplace.validate(data);
        if (!validation.isValid && validation.errors.length > 0) {
          return (
            <div style={{ padding: 'var(--spacing-lg)', borderTop: '1px solid var(--border)', flexShrink: 0 }}>
              <div className="bg-[var(--danger)]/10 border border-[var(--danger)]/30 rounded-lg p-3">
                <div className="flex items-start gap-2">
                  <i className="ri-error-warning-line text-[var(--danger)] text-lg mt-0.5"></i>
                  <div className="flex-1">
                    <div className="text-sm font-medium text-[var(--danger)] mb-1">
                      Validation Errors
                    </div>
                    <ul className="list-disc list-inside space-y-1 text-xs text-[var(--text-primary)]">
                      {validation.errors.map((error, index) => (
                        <li key={index}>
                          <strong>{error.field}:</strong> {error.message}
                        </li>
                      ))}
                    </ul>
                  </div>
                </div>
              </div>
            </div>
          );
        }
        return null;
      })()}
    </div>
  );
}
