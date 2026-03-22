import React from 'react';
import { ProductOption } from './types';

interface Props {
  options: ProductOption[];
  onChange: (options: ProductOption[]) => void;
  onGenerateVariants: () => void;
  hasGeneratedVariants: boolean;
}

export default function VariantOptionsSection({ 
  options, 
  onChange, 
  onGenerateVariants,
  hasGeneratedVariants 
}: Props) {
  // Store the raw input strings to allow typing commas
  const [inputValues, setInputValues] = React.useState<string[]>(
    options.map(opt => opt.values.join(', '))
  );

  // Update input values when options change externally
  React.useEffect(() => {
    setInputValues(options.map(opt => opt.values.join(', ')));
  }, [options.length]); // Only update when options array length changes
  
  const handleOptionChange = (index: number, field: 'name' | 'values', value: string) => {
    const newOptions = [...options];
    if (field === 'values') {
      // Update the input string state
      const newInputValues = [...inputValues];
      newInputValues[index] = value;
      setInputValues(newInputValues);
      
      // Update the options with parsed values
      newOptions[index].values = value.split(',').map(v => v.trim()).filter(Boolean);
    } else {
      newOptions[index].name = value;
    }
    onChange(newOptions);
  };

  const addOption = () => {
    onChange([...options, { name: '', values: [] }]);
    setInputValues([...inputValues, '']);
  };

  const removeOption = (index: number) => {
    if (options.length > 1) {
      onChange(options.filter((_, i) => i !== index));
      setInputValues(inputValues.filter((_, i) => i !== index));
    }
  };

  const canGenerate = options.every(opt => opt.name && opt.values.length > 0);

  return (
    <div className="card" style={{ marginBottom: 'var(--spacing-lg)' }}>
      <div className="card-header">
        <div>
          <h2 style={{ fontSize: '18px', fontWeight: 600 }}>Product Options</h2>
          <p style={{ fontSize: '13px', color: 'var(--text-muted)', marginTop: '4px' }}>
            Define product variations (e.g., Color, Size)
          </p>
        </div>
      </div>
      <div style={{ padding: 'var(--spacing-xl)' }}>
        <div className="space-y-3">
          {options.map((option, index) => (
            <div key={index} className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-xs font-medium text-[var(--text-primary)] mb-1">
                  Option Name
                </label>
                <input
                  type="text"
                  placeholder="e.g., Color, Size, Material"
                  value={option.name}
                  onChange={(e) => handleOptionChange(index, 'name', e.target.value)}
                  className="input w-full"
                />
              </div>
              <div className="flex gap-2">
                <div className="flex-1">
                  <label className="block text-xs font-medium text-[var(--text-primary)] mb-1">
                    Values (comma separated)
                  </label>
                  <input
                    type="text"
                    placeholder="e.g., Red, Blue, Green"
                    value={inputValues[index] || ''}
                    onChange={(e) => handleOptionChange(index, 'values', e.target.value)}
                    className="input w-full"
                  />
                </div>
                <div className="flex items-end">
                  <button
                    type="button"
                    onClick={() => removeOption(index)}
                    disabled={options.length === 1}
                    className="px-3 py-2 text-[var(--danger)] hover:bg-[var(--danger)]/10 rounded-lg transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
                  >
                    <i className="ri-delete-bin-line"></i>
                  </button>
                </div>
              </div>
            </div>
          ))}

          <div className="flex items-center justify-between pt-2">
            <button
              type="button"
              onClick={addOption}
              className="text-[var(--primary)] hover:text-[var(--primary)]/80 font-medium cursor-pointer text-sm"
            >
              + Add Option
            </button>

            <button
              type="button"
              onClick={onGenerateVariants}
              disabled={!canGenerate}
              className="btn btn-primary"
            >
              <i className="ri-refresh-line mr-2"></i>
              {hasGeneratedVariants ? 'Regenerate' : 'Generate'} Variants
            </button>
          </div>

          {hasGeneratedVariants && (
            <div className="bg-[var(--warning)]/10 border border-[var(--warning)]/30 rounded-lg p-3 flex items-start gap-3">
              <i className="ri-alert-line text-[var(--warning)] text-lg mt-0.5"></i>
              <div className="text-sm text-[var(--text-primary)]">
                <div className="font-medium mb-1">Warning</div>
                Changing options will regenerate all variants. Custom variant data will be reset.
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
