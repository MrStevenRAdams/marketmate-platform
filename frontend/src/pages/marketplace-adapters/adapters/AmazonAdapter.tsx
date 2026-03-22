import React, { useState } from 'react';
import { MarketplaceFormProps } from '../types';

export default function AmazonAdapter({ coreData, marketplaceData, onChange, onSync }: MarketplaceFormProps) {
  const [activeTab, setActiveTab] = useState<'listing' | 'pricing' | 'fulfillment' | 'compliance'>('listing');

  const updateField = (field: string, value: any) => {
    onChange({ ...marketplaceData, [field]: value });
  };

  const updateBulletPoint = (index: number, value: string) => {
    const newBulletPoints = [...(marketplaceData.bulletPoints || ['', '', '', '', ''])];
    newBulletPoints[index] = value;
    updateField('bulletPoints', newBulletPoints);
  };

  return (
    <div className="space-y-4">
      {/* Tabs */}
      <div className="flex gap-2 border-b border-[var(--border)]">
        <button
          onClick={() => setActiveTab('listing')}
          className={`px-4 py-2 text-sm font-medium transition-colors ${
            activeTab === 'listing'
              ? 'text-[var(--primary)] border-b-2 border-[var(--primary)]'
              : 'text-[var(--text-muted)] hover:text-[var(--text-primary)]'
          }`}
        >
          Listing
        </button>
        <button
          onClick={() => setActiveTab('pricing')}
          className={`px-4 py-2 text-sm font-medium transition-colors ${
            activeTab === 'pricing'
              ? 'text-[var(--primary)] border-b-2 border-[var(--primary)]'
              : 'text-[var(--text-muted)] hover:text-[var(--text-primary)]'
          }`}
        >
          Pricing
        </button>
        <button
          onClick={() => setActiveTab('fulfillment')}
          className={`px-4 py-2 text-sm font-medium transition-colors ${
            activeTab === 'fulfillment'
              ? 'text-[var(--primary)] border-b-2 border-[var(--primary)]'
              : 'text-[var(--text-muted)] hover:text-[var(--text-primary)]'
          }`}
        >
          Fulfillment
        </button>
        <button
          onClick={() => setActiveTab('compliance')}
          className={`px-4 py-2 text-sm font-medium transition-colors ${
            activeTab === 'compliance'
              ? 'text-[var(--primary)] border-b-2 border-[var(--primary)]'
              : 'text-[var(--text-muted)] hover:text-[var(--text-primary)]'
          }`}
        >
          Compliance
        </button>
      </div>

      {/* Sync Button */}
      <button
        onClick={onSync}
        className="w-full btn btn-secondary flex items-center justify-center gap-2"
      >
        <i className="ri-refresh-line"></i>
        Sync from Core Product Data
      </button>

      {/* Tab Content */}
      <div className="space-y-4">
        {activeTab === 'listing' && (
          <>
            {/* Product ID */}
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
                  Product ID Type
                </label>
                <select
                  value={marketplaceData.productIdType || 'UPC'}
                  onChange={(e) => updateField('productIdType', e.target.value)}
                  className="select w-full"
                >
                  <option value="UPC">UPC</option>
                  <option value="EAN">EAN</option>
                  <option value="ASIN">ASIN</option>
                  <option value="ISBN">ISBN</option>
                  <option value="GCID">GCID</option>
                </select>
              </div>
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
                  Product ID *
                </label>
                <input
                  type="text"
                  value={marketplaceData.productId || ''}
                  onChange={(e) => updateField('productId', e.target.value)}
                  className="input w-full"
                  placeholder="Enter product ID"
                  style={{ fontFamily: 'monospace' }}
                />
              </div>
            </div>

            {/* Title */}
            <div>
              <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
                Amazon Title *
                <span className="text-xs text-[var(--text-muted)] ml-2">(Max 200 chars, 60-80 recommended)</span>
              </label>
              <input
                type="text"
                value={marketplaceData.title || ''}
                onChange={(e) => updateField('title', e.target.value)}
                className="input w-full"
                placeholder="Product title optimized for Amazon search"
                maxLength={200}
              />
              <div className="text-xs text-[var(--text-muted)] mt-1">
                {(marketplaceData.title || '').length}/200 characters
              </div>
            </div>

            {/* Bullet Points */}
            <div>
              <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
                Key Product Features (Bullet Points) *
                <span className="text-xs text-[var(--text-muted)] ml-2">(Max 5, each max 255 chars)</span>
              </label>
              <div className="space-y-2">
                {[0, 1, 2, 3, 4].map((index) => (
                  <div key={index} className="flex items-center gap-2">
                    <span className="text-[var(--text-muted)] text-sm">{index + 1}.</span>
                    <input
                      type="text"
                      value={marketplaceData.bulletPoints?.[index] || ''}
                      onChange={(e) => updateBulletPoint(index, e.target.value)}
                      className="input flex-1"
                      placeholder={`Bullet point ${index + 1}`}
                      maxLength={255}
                    />
                  </div>
                ))}
              </div>
            </div>

            {/* Description */}
            <div>
              <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
                Product Description
              </label>
              <textarea
                value={marketplaceData.description || ''}
                onChange={(e) => updateField('description', e.target.value)}
                className="input w-full"
                rows={6}
                placeholder="Detailed product description (HTML allowed)"
              />
            </div>

            {/* Search Terms */}
            <div>
              <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
                Search Terms
                <span className="text-xs text-[var(--text-muted)] ml-2">(Keywords for Amazon search)</span>
              </label>
              <input
                type="text"
                value={marketplaceData.searchTerms || ''}
                onChange={(e) => updateField('searchTerms', e.target.value)}
                className="input w-full"
                placeholder="keyword1, keyword2, keyword3"
              />
            </div>

            {/* Browse Node */}
            <div>
              <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
                Browse Node (Category ID)
              </label>
              <input
                type="text"
                value={marketplaceData.browseNode || ''}
                onChange={(e) => updateField('browseNode', e.target.value)}
                className="input w-full"
                placeholder="Amazon category node ID"
              />
            </div>
          </>
        )}

        {activeTab === 'pricing' && (
          <>
            {/* Price */}
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
                  Price * ($)
                </label>
                <input
                  type="number"
                  step="0.01"
                  value={marketplaceData.price || ''}
                  onChange={(e) => updateField('price', parseFloat(e.target.value) || 0)}
                  className="input w-full"
                  placeholder="0.00"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
                  Sale Price ($)
                </label>
                <input
                  type="number"
                  step="0.01"
                  value={marketplaceData.salePrice || ''}
                  onChange={(e) => updateField('salePrice', parseFloat(e.target.value) || 0)}
                  className="input w-full"
                  placeholder="0.00"
                />
              </div>
            </div>

            {/* Currency */}
            <div>
              <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
                Currency
              </label>
              <select
                value={marketplaceData.currency || 'USD'}
                onChange={(e) => updateField('currency', e.target.value)}
                className="select w-full"
              >
                <option value="USD">USD ($)</option>
                <option value="CAD">CAD ($)</option>
                <option value="MXN">MXN ($)</option>
              </select>
            </div>
          </>
        )}

        {activeTab === 'fulfillment' && (
          <>
            {/* Fulfillment Channel */}
            <div>
              <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
                Fulfillment Channel *
              </label>
              <select
                value={marketplaceData.fulfillmentChannel || 'FBA'}
                onChange={(e) => updateField('fulfillmentChannel', e.target.value)}
                className="select w-full"
              >
                <option value="FBA">FBA (Fulfilled by Amazon)</option>
                <option value="FBM">FBM (Fulfilled by Merchant)</option>
              </select>
            </div>

            {/* Quantity (for FBM) */}
            {marketplaceData.fulfillmentChannel === 'FBM' && (
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
                  Quantity
                </label>
                <input
                  type="number"
                  value={marketplaceData.quantity || ''}
                  onChange={(e) => updateField('quantity', parseInt(e.target.value) || 0)}
                  className="input w-full"
                  placeholder="0"
                />
              </div>
            )}

            {/* Handling Time */}
            <div>
              <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
                Handling Time (days)
              </label>
              <input
                type="number"
                value={marketplaceData.handlingTime || ''}
                onChange={(e) => updateField('handlingTime', parseInt(e.target.value) || 0)}
                className="input w-full"
                placeholder="1-30"
              />
            </div>
          </>
        )}

        {activeTab === 'compliance' && (
          <>
            {/* Condition */}
            <div>
              <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
                Condition *
              </label>
              <select
                value={marketplaceData.condition || 'New'}
                onChange={(e) => updateField('condition', e.target.value)}
                className="select w-full"
              >
                <option value="New">New</option>
                <option value="Refurbished">Refurbished</option>
                <option value="Used - Like New">Used - Like New</option>
                <option value="Used - Very Good">Used - Very Good</option>
                <option value="Used - Good">Used - Good</option>
                <option value="Used - Acceptable">Used - Acceptable</option>
              </select>
            </div>

            {/* Condition Note */}
            {marketplaceData.condition !== 'New' && (
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
                  Condition Note
                </label>
                <textarea
                  value={marketplaceData.conditionNote || ''}
                  onChange={(e) => updateField('conditionNote', e.target.value)}
                  className="input w-full"
                  rows={3}
                  placeholder="Describe the condition"
                  maxLength={1000}
                />
              </div>
            )}

            {/* Safety Warning */}
            <div>
              <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
                Safety Warning
              </label>
              <textarea
                value={marketplaceData.safetyWarning || ''}
                onChange={(e) => updateField('safetyWarning', e.target.value)}
                className="input w-full"
                rows={2}
                placeholder="Any safety warnings or choking hazards"
              />
            </div>
          </>
        )}
      </div>

      {/* Info Box */}
      <div className="bg-[var(--bg-tertiary)] rounded-lg p-3 border border-[var(--border)]">
        <div className="flex items-start gap-2">
          <i className="ri-information-line text-[var(--primary)] text-lg mt-0.5"></i>
          <div className="text-sm text-[var(--text-muted)]">
            <div className="font-medium text-[var(--text-primary)] mb-1">Amazon Tips</div>
            <ul className="list-disc list-inside space-y-1 text-xs">
              <li>Keep title under 80 characters for mobile visibility</li>
              <li>Use all 5 bullet points - they improve conversion</li>
              <li>Include relevant keywords in title and search terms</li>
              <li>FBA products are eligible for Prime shipping</li>
            </ul>
          </div>
        </div>
      </div>
    </div>
  );
}
