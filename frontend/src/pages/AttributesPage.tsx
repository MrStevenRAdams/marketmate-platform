import { useState, useEffect } from 'react';
import { attributeService, attributeSetService } from '../services/api';

type AttributeDataType = 'text' | 'number' | 'boolean' | 'date' | 'list' | 'multiselect';

interface AttributeOption {
  id: string;
  value: string;
  label: string;
}

interface Attribute {
  id: string;
  name: string;
  code: string;
  dataType: AttributeDataType;
  required: boolean;
  options?: AttributeOption[];
  defaultValue?: string;
  description?: string;
}

interface AttributeSet {
  id: string;
  name: string;
  code: string;
  description?: string;
  attributeIds: string[];
}

const getDataTypeIcon = (type: AttributeDataType): string => {
  const icons = {
    text: 'ri-text',
    number: 'ri-hashtag',
    boolean: 'ri-checkbox-line',
    date: 'ri-calendar-line',
    list: 'ri-list-check',
    multiselect: 'ri-list-check-2'
  };
  return icons[type] || 'ri-text';
};

const getDataTypeLabel = (type: AttributeDataType): string => {
  const labels = {
    text: 'Text',
    number: 'Number',
    boolean: 'Boolean',
    date: 'Date',
    list: 'Dropdown',
    multiselect: 'Multi-select'
  };
  return labels[type] || 'Text';
};

export default function AttributesPage() {
  const [activeTab, setActiveTab] = useState<'attributes' | 'sets'>('attributes');
  const [attributes, setAttributes] = useState<Attribute[]>([]);
  const [attributeSets, setAttributeSets] = useState<AttributeSet[]>([]);
  const [showAttributeModal, setShowAttributeModal] = useState(false);
  const [showSetModal, setShowSetModal] = useState(false);
  const [showSetEditModal, setShowSetEditModal] = useState(false);
  const [editingAttribute, setEditingAttribute] = useState<Attribute | null>(null);
  const [editingSet, setEditingSet] = useState<AttributeSet | null>(null);
  const [isLoading, setIsLoading] = useState(false);

  const [attributeForm, setAttributeForm] = useState<Partial<Attribute>>({
    name: '',
    code: '',
    dataType: 'text',
    required: false,
    options: [],
    description: ''
  });

  const [setForm, setSetForm] = useState<Partial<AttributeSet>>({
    name: '',
    code: '',
    description: '',
    attributeIds: []
  });

  useEffect(() => {
    loadAttributes();
    loadAttributeSets();
  }, []);

  async function loadAttributes() {
    setIsLoading(true);
    try {
      const response = await attributeService.list();
      const data = response.data?.data?.data || response.data?.data || response.data || [];
      setAttributes(Array.isArray(data) ? data : []);
    } catch (error) {
      console.error('Failed to load attributes:', error);
      setAttributes([]);
    } finally {
      setIsLoading(false);
    }
  };

  async function loadAttributeSets() {
    try {
      const response = await attributeSetService.list();
      const data = response.data?.data?.data || response.data?.data || response.data || [];
      setAttributeSets(Array.isArray(data) ? data : []);
    } catch (error) {
      console.error('Failed to load attribute sets:', error);
      setAttributeSets([]);
    }
  };

  const handleCreateAttribute = () => {
    setEditingAttribute(null);
    setAttributeForm({
      name: '',
      code: '',
      dataType: 'text',
      required: false,
      options: [],
      description: ''
    });
    setShowAttributeModal(true);
  };

  const handleEditAttribute = (attribute: Attribute) => {
    setEditingAttribute(attribute);
    setAttributeForm(attribute);
    setShowAttributeModal(true);
  };

  const handleSaveAttribute = async () => {
    if (!attributeForm.name || !attributeForm.code) {
      alert('Please fill in all required fields');
      return;
    }

    if ((attributeForm.dataType === 'list' || attributeForm.dataType === 'multiselect') && 
        (!attributeForm.options || attributeForm.options.length === 0)) {
      alert('Please add at least one option for list/multiselect types');
      return;
    }

    try {
      if (editingAttribute) {
        await attributeService.update(editingAttribute.id, attributeForm);
      } else {
        await attributeService.create(attributeForm);
      }

      await loadAttributes();
      setShowAttributeModal(false);
      setAttributeForm({
        name: '',
        code: '',
        dataType: 'text',
        required: false,
        options: [],
        description: ''
      });
    } catch (error) {
      console.error('Failed to save attribute:', error);
      alert('Failed to save attribute');
    }
  };

  const handleDeleteAttribute = async (id: string) => {
    if (!confirm('Are you sure you want to delete this attribute? It will be removed from all attribute sets.')) return;
    
    try {
      await attributeService.delete(id);
      await loadAttributes();
      await loadAttributeSets(); // Refresh sets in case they referenced this attribute
    } catch (error) {
      console.error('Failed to delete attribute:', error);
      alert('Failed to delete attribute');
    }
  };

  const handleCreateSet = () => {
    setEditingSet(null);
    setSetForm({
      name: '',
      code: '',
      description: '',
      attributeIds: []
    });
    setShowSetModal(true);
  };

  const handleEditSet = (set: AttributeSet) => {
    setEditingSet(set);
    setSetForm({
      ...set,
      attributeIds: set.attributeIds || []
    });
    setShowSetEditModal(true);
  };

  const handleSaveSet = async () => {
    if (!setForm.name || !setForm.code) {
      alert('Please fill in all required fields');
      return;
    }

    try {
      if (editingSet) {
        await attributeSetService.update(editingSet.id, setForm);
        await loadAttributeSets();
        setShowSetEditModal(false);
      } else {
        const response = await attributeSetService.create(setForm);
        const newSet = response.data?.data || response.data;
        await loadAttributeSets();
        setShowSetModal(false);
        
        // Immediately open edit modal to add attributes
        setEditingSet(newSet);
        setSetForm(newSet);
        setShowSetEditModal(true);
      }

      if (editingSet) {
        setSetForm({
          name: '',
          code: '',
          description: '',
          attributeIds: []
        });
      }
    } catch (error) {
      console.error('Failed to save attribute set:', error);
      alert('Failed to save attribute set');
    }
  };

  const handleDeleteSet = async (id: string) => {
    if (!confirm('Are you sure you want to delete this attribute set?')) return;
    
    try {
      await attributeSetService.delete(id);
      await loadAttributeSets();
    } catch (error) {
      console.error('Failed to delete attribute set:', error);
      alert('Failed to delete attribute set');
    }
  };

  const handleAddOption = () => {
    const newOption: AttributeOption = {
      id: Date.now().toString(),
      value: '',
      label: ''
    };
    setAttributeForm({
      ...attributeForm,
      options: [...(attributeForm.options || []), newOption]
    });
  };

  const handleUpdateOption = (optionId: string, field: 'value' | 'label', value: string) => {
    setAttributeForm({
      ...attributeForm,
      options: attributeForm.options?.map(opt => 
        opt.id === optionId ? { ...opt, [field]: value } : opt
      )
    });
  };

  const handleRemoveOption = (optionId: string) => {
    setAttributeForm({
      ...attributeForm,
      options: attributeForm.options?.filter(opt => opt.id !== optionId)
    });
  };

  const handleAddAttributeToSet = (attributeId: string) => {
    if (!setForm.attributeIds!.includes(attributeId)) {
      setSetForm({
        ...setForm,
        attributeIds: [...setForm.attributeIds!, attributeId]
      });
    }
  };

  const handleRemoveAttributeFromSet = (attributeId: string) => {
    setSetForm({
      ...setForm,
      attributeIds: setForm.attributeIds!.filter(id => id !== attributeId)
    });
  };

  const availableAttributes = attributes.filter(
    attr => !setForm.attributeIds?.includes(attr.id)
  );

  const setAttributes_selected = setForm.attributeIds?.map(id => 
    attributes.find(attr => attr.id === id)
  ).filter(Boolean) as Attribute[];

  return (
    <div style={{ minHeight: '100vh', backgroundColor: '#f9fafb' }}>
      {/* Header */}
      <div style={{ 
        backgroundColor: 'white', 
        borderBottom: '1px solid #e5e7eb', 
        padding: '1rem 1.5rem' 
      }}>
        <h1 style={{ fontSize: '1.5rem', fontWeight: 'bold', color: '#111827', margin: 0 }}>
          Attributes & Attribute Sets
        </h1>
        <p style={{ fontSize: '0.875rem', color: '#6b7280', marginTop: '0.25rem' }}>
          Manage product attributes and attribute sets
        </p>
      </div>

      {/* Tabs */}
      <div style={{ maxWidth: '1280px', margin: '0 auto', padding: '1.5rem' }}>
        <div style={{ backgroundColor: 'white', borderRadius: '0.5rem', marginBottom: '1rem', boxShadow: '0 1px 3px 0 rgb(0 0 0 / 0.1)' }}>
          <div style={{ borderBottom: '1px solid #e5e7eb' }}>
            <nav style={{ display: 'flex' }}>
              <button
                onClick={() => setActiveTab('attributes')}
                style={{
                  padding: '0.625rem 1.5rem',
                  fontSize: '0.875rem',
                  fontWeight: '500',
                  borderTop: 'none',
                  borderLeft: 'none',
                  borderRight: 'none',
                  borderBottomWidth: '2px',
                  borderBottomStyle: 'solid',
                  borderBottomColor: activeTab === 'attributes' ? '#14b8a6' : 'transparent',
                  color: activeTab === 'attributes' ? '#14b8a6' : '#6b7280',
                  cursor: 'pointer',
                  background: 'none'
                }}
              >
                Attributes
              </button>
              <button
                onClick={() => setActiveTab('sets')}
                style={{
                  padding: '0.625rem 1.5rem',
                  fontSize: '0.875rem',
                  fontWeight: '500',
                  borderTop: 'none',
                  borderLeft: 'none',
                  borderRight: 'none',
                  borderBottomWidth: '2px',
                  borderBottomStyle: 'solid',
                  borderBottomColor: activeTab === 'sets' ? '#14b8a6' : 'transparent',
                  color: activeTab === 'sets' ? '#14b8a6' : '#6b7280',
                  cursor: 'pointer',
                  background: 'none'
                }}
              >
                Attribute Sets
              </button>
            </nav>
          </div>
        </div>

        {/* Content */}
        {activeTab === 'attributes' ? (
          <div style={{ backgroundColor: 'white', borderRadius: '0.5rem', boxShadow: '0 1px 3px 0 rgb(0 0 0 / 0.1)' }}>
            <div style={{ padding: '1rem', borderBottom: '1px solid #e5e7eb', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <h2 style={{ fontSize: '1rem', fontWeight: '600', color: '#111827', margin: 0 }}>Attributes</h2>
              <button
                onClick={handleCreateAttribute}
                style={{
                  padding: '0.375rem 0.75rem',
                  backgroundColor: '#14b8a6',
                  color: 'white',
                  fontSize: '0.875rem',
                  borderRadius: '0.375rem',
                  border: 'none',
                  cursor: 'pointer',
                  display: 'flex',
                  alignItems: 'center',
                  gap: '0.25rem'
                }}
              >
                <i className="ri-add-line"></i>
                Add Attribute
              </button>
            </div>
            
            <div style={{ overflowX: 'auto' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead style={{ backgroundColor: '#f9fafb', borderBottom: '1px solid #e5e7eb' }}>
                  <tr>
                    <th style={{ padding: '0.5rem 1rem', textAlign: 'left', fontSize: '0.75rem', fontWeight: '500', color: '#374151', textTransform: 'uppercase' }}>Name</th>
                    <th style={{ padding: '0.5rem 1rem', textAlign: 'left', fontSize: '0.75rem', fontWeight: '500', color: '#374151', textTransform: 'uppercase' }}>Code</th>
                    <th style={{ padding: '0.5rem 1rem', textAlign: 'left', fontSize: '0.75rem', fontWeight: '500', color: '#374151', textTransform: 'uppercase' }}>Type</th>
                    <th style={{ padding: '0.5rem 1rem', textAlign: 'left', fontSize: '0.75rem', fontWeight: '500', color: '#374151', textTransform: 'uppercase' }}>Required</th>
                    <th style={{ padding: '0.5rem 1rem', textAlign: 'left', fontSize: '0.75rem', fontWeight: '500', color: '#374151', textTransform: 'uppercase' }}>Options</th>
                    <th style={{ padding: '0.5rem 1rem', textAlign: 'right', fontSize: '0.75rem', fontWeight: '500', color: '#374151', textTransform: 'uppercase' }}>Actions</th>
                  </tr>
                </thead>
                <tbody style={{ borderTop: '1px solid #e5e7eb' }}>
                  {attributes.map((attr) => (
                    <tr key={attr.id} style={{ borderBottom: '1px solid #e5e7eb' }}>
                      <td style={{ padding: '0.5rem 1rem', fontSize: '0.875rem', color: '#111827' }}>{attr.name}</td>
                      <td style={{ padding: '0.5rem 1rem', fontSize: '0.875rem', color: '#6b7280', fontFamily: 'monospace' }}>{attr.code}</td>
                      <td style={{ padding: '0.5rem 1rem', fontSize: '0.875rem' }}>
                        <span style={{
                          display: 'inline-flex',
                          alignItems: 'center',
                          padding: '0.125rem 0.5rem',
                          borderRadius: '0.25rem',
                          fontSize: '0.75rem',
                          fontWeight: '500',
                          backgroundColor: '#dbeafe',
                          color: '#1e40af'
                        }}>
                          <i className={`${getDataTypeIcon(attr.dataType)}`} style={{ marginRight: '0.25rem' }}></i>
                          {getDataTypeLabel(attr.dataType)}
                        </span>
                      </td>
                      <td style={{ padding: '0.5rem 1rem', fontSize: '0.875rem' }}>
                        {attr.required ? (
                          <span style={{
                            display: 'inline-flex',
                            padding: '0.125rem 0.5rem',
                            borderRadius: '0.25rem',
                            fontSize: '0.75rem',
                            fontWeight: '500',
                            backgroundColor: '#fee2e2',
                            color: '#991b1b'
                          }}>Required</span>
                        ) : (
                          <span style={{
                            display: 'inline-flex',
                            padding: '0.125rem 0.5rem',
                            borderRadius: '0.25rem',
                            fontSize: '0.75rem',
                            fontWeight: '500',
                            backgroundColor: '#f3f4f6',
                            color: '#1f2937'
                          }}>Optional</span>
                        )}
                      </td>
                      <td style={{ padding: '0.5rem 1rem', fontSize: '0.875rem', color: '#6b7280' }}>
                        {attr.options && attr.options.length > 0 ? (
                          <span style={{ fontSize: '0.75rem' }}>{attr.options.length} options</span>
                        ) : (
                          <span style={{ fontSize: '0.75rem', color: '#9ca3af' }}>—</span>
                        )}
                      </td>
                      <td style={{ padding: '0.5rem 1rem', fontSize: '0.875rem', textAlign: 'right' }}>
                        <button
                          type="button"
                          onClick={() => handleEditAttribute(attr)}
                          style={{
                            padding: '0.375rem 0.75rem',
                            marginRight: '0.5rem',
                            border: '1px solid #14b8a6',
                            borderRadius: '0.375rem',
                            backgroundColor: 'white',
                            color: '#14b8a6',
                            cursor: 'pointer',
                            fontSize: '0.875rem',
                            display: 'inline-flex',
                            alignItems: 'center',
                            gap: '0.25rem'
                          }}
                          title="Edit attribute"
                          onMouseEnter={(e) => {
                            e.currentTarget.style.backgroundColor = '#14b8a6';
                            e.currentTarget.style.color = 'white';
                          }}
                          onMouseLeave={(e) => {
                            e.currentTarget.style.backgroundColor = 'white';
                            e.currentTarget.style.color = '#14b8a6';
                          }}
                        >
                          <i className="ri-edit-line"></i>
                          Edit
                        </button>
                        <button
                          type="button"
                          onClick={() => handleDeleteAttribute(attr.id)}
                          style={{
                            padding: '0.375rem 0.75rem',
                            border: '1px solid #dc2626',
                            borderRadius: '0.375rem',
                            backgroundColor: 'white',
                            color: '#dc2626',
                            cursor: 'pointer',
                            fontSize: '0.875rem',
                            display: 'inline-flex',
                            alignItems: 'center',
                            gap: '0.25rem'
                          }}
                          title="Delete attribute"
                          onMouseEnter={(e) => {
                            e.currentTarget.style.backgroundColor = '#dc2626';
                            e.currentTarget.style.color = 'white';
                          }}
                          onMouseLeave={(e) => {
                            e.currentTarget.style.backgroundColor = 'white';
                            e.currentTarget.style.color = '#dc2626';
                          }}
                        >
                          <i className="ri-delete-bin-line"></i>
                          Delete
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {attributes.length === 0 && (
                <div style={{ textAlign: 'center', padding: '2rem', color: '#6b7280', fontSize: '0.875rem' }}>
                  No attributes yet. Click "Add Attribute" to create one.
                </div>
              )}
            </div>
          </div>
        ) : (
          <div>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem' }}>
              <h2 style={{ fontSize: '1rem', fontWeight: '600', color: '#111827', margin: 0 }}>Attribute Sets</h2>
              <button
                onClick={handleCreateSet}
                style={{
                  padding: '0.375rem 0.75rem',
                  backgroundColor: '#14b8a6',
                  color: 'white',
                  fontSize: '0.875rem',
                  borderRadius: '0.375rem',
                  border: 'none',
                  cursor: 'pointer',
                  display: 'flex',
                  alignItems: 'center',
                  gap: '0.25rem'
                }}
              >
                <i className="ri-add-line"></i>
                Add Attribute Set
              </button>
            </div>
            
            <div style={{ 
              display: 'grid', 
              gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))',
              gap: '1rem'
            }}>
              {attributeSets.map((set) => (
                <div key={set.id} style={{ 
                  backgroundColor: 'white', 
                  borderRadius: '0.5rem', 
                  boxShadow: '0 1px 3px 0 rgb(0 0 0 / 0.1)', 
                  border: '1px solid #e5e7eb',
                  padding: '1rem'
                }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: '0.75rem' }}>
                    <div>
                      <h3 style={{ fontSize: '1rem', fontWeight: '600', color: '#111827', margin: 0 }}>{set.name}</h3>
                      <p style={{ fontSize: '0.75rem', color: '#6b7280', fontFamily: 'monospace', marginTop: '0.125rem' }}>{set.code}</p>
                    </div>
                    <div style={{ display: 'flex', gap: '0.5rem' }}>
                      <button
                        onClick={() => handleEditSet(set)}
                        style={{
                          padding: '0.375rem 0.75rem',
                          backgroundColor: '#2563eb',
                          color: 'white',
                          borderRadius: '0.5rem',
                          border: 'none',
                          cursor: 'pointer',
                          fontSize: '0.875rem',
                          display: 'flex',
                          alignItems: 'center',
                          gap: '0.25rem'
                        }}
                      >
                        <i className="ri-edit-line"></i>
                        Edit
                      </button>
                      <button
                        onClick={() => handleDeleteSet(set.id)}
                        style={{
                          padding: '0.375rem 0.75rem',
                          backgroundColor: '#dc2626',
                          color: 'white',
                          borderRadius: '0.5rem',
                          border: 'none',
                          cursor: 'pointer',
                          fontSize: '0.875rem',
                          display: 'flex',
                          alignItems: 'center',
                          gap: '0.25rem'
                        }}
                      >
                        <i className="ri-delete-bin-line"></i>
                        Delete
                      </button>
                    </div>
                  </div>
                  {set.description && (
                    <p style={{ fontSize: '0.75rem', color: '#4b5563', marginBottom: '0.75rem' }}>{set.description}</p>
                  )}
                  <div style={{ borderTop: '1px solid #e5e7eb', paddingTop: '0.75rem' }}>
                    <div style={{ fontSize: '0.75rem', color: '#4b5563' }}>
                      <span style={{ fontWeight: '500' }}>{(set.attributeIds || []).length}</span> attributes in this set
                    </div>
                  </div>
                </div>
              ))}
            </div>
            
            {attributeSets.length === 0 && (
              <div style={{ 
                backgroundColor: 'white', 
                borderRadius: '0.5rem', 
                boxShadow: '0 1px 3px 0 rgb(0 0 0 / 0.1)', 
                border: '1px solid #e5e7eb',
                textAlign: 'center',
                padding: '3rem',
                color: '#6b7280',
                fontSize: '0.875rem'
              }}>
                No attribute sets yet. Click "Add Attribute Set" to create one.
              </div>
            )}
          </div>
        )}
      </div>

      {/* Attribute Modal */}
      {showAttributeModal && (
        <div 
          style={{
            position: 'fixed',
            inset: 0,
            backgroundColor: 'rgba(0, 0, 0, 0.5)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            zIndex: 50,
            padding: '1rem'
          }}
          onClick={() => setShowAttributeModal(false)}
        >
          <div 
            style={{
              backgroundColor: 'white',
              borderRadius: '0.5rem',
              boxShadow: '0 20px 25px -5px rgb(0 0 0 / 0.1)',
              maxWidth: '42rem',
              width: '100%',
              maxHeight: '90vh',
              overflowY: 'auto'
            }}
            onClick={(e) => e.stopPropagation()}
          >
            <div style={{ padding: '1rem', borderBottom: '1px solid #e5e7eb', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <h3 style={{ fontSize: '1.125rem', fontWeight: '600', color: '#111827', margin: 0 }}>
                {editingAttribute ? 'Edit Attribute' : 'Add Attribute'}
              </h3>
              <button
                onClick={() => setShowAttributeModal(false)}
                style={{
                  color: '#9ca3af',
                  cursor: 'pointer',
                  background: 'none',
                  border: 'none',
                  fontSize: '1.25rem'
                }}
              >
                <i className="ri-close-line"></i>
              </button>
            </div>
            
            <div style={{ padding: '1rem' }}>
              <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
                <div>
                  <label style={{ display: 'block', fontSize: '0.875rem', fontWeight: '500', color: '#374151', marginBottom: '0.25rem' }}>
                    Attribute Name <span style={{ color: '#dc2626' }}>*</span>
                  </label>
                  <input
                    type="text"
                    value={attributeForm.name}
                    onChange={(e) => {
                      const newName = e.target.value;
                      setAttributeForm({ 
                        ...attributeForm, 
                        name: newName,
                        // Auto-populate code if it's empty
                        code: attributeForm.code === '' ? newName.toLowerCase().replace(/\s+/g, '_') : attributeForm.code
                      });
                    }}
                    style={{
                      width: '100%',
                      padding: '0.375rem 0.75rem',
                      fontSize: '0.875rem',
                      border: '1px solid #d1d5db',
                      borderRadius: '0.375rem'
                    }}
                    placeholder="e.g., Color, Size, Material"
                  />
                </div>

                <div>
                  <label style={{ display: 'block', fontSize: '0.875rem', fontWeight: '500', color: '#374151', marginBottom: '0.25rem' }}>
                    Attribute Code <span style={{ color: '#dc2626' }}>*</span>
                  </label>
                  <input
                    type="text"
                    value={attributeForm.code}
                    onChange={(e) => setAttributeForm({ ...attributeForm, code: e.target.value.toLowerCase().replace(/\s+/g, '_') })}
                    style={{
                      width: '100%',
                      padding: '0.375rem 0.75rem',
                      fontSize: '0.875rem',
                      border: '1px solid #d1d5db',
                      borderRadius: '0.375rem',
                      fontFamily: 'monospace'
                    }}
                    placeholder="e.g., color, size, material"
                  />
                </div>

                <div>
                  <label style={{ display: 'block', fontSize: '0.875rem', fontWeight: '500', color: '#374151', marginBottom: '0.25rem' }}>
                    Data Type
                  </label>
                  <select
                    value={attributeForm.dataType}
                    onChange={(e) => {
                      const newType = e.target.value as AttributeDataType;
                      setAttributeForm({ 
                        ...attributeForm, 
                        dataType: newType,
                        // Initialize options array if switching to list/multiselect
                        options: (newType === 'list' || newType === 'multiselect') && !attributeForm.options 
                          ? [] 
                          : attributeForm.options
                      });
                    }}
                    style={{
                      width: '100%',
                      padding: '0.375rem 0.75rem',
                      fontSize: '0.875rem',
                      border: '1px solid #d1d5db',
                      borderRadius: '0.375rem'
                    }}
                  >
                    <option value="text">Text</option>
                    <option value="number">Number</option>
                    <option value="boolean">Boolean</option>
                    <option value="date">Date</option>
                    <option value="list">Dropdown List</option>
                    <option value="multiselect">Multi-select</option>
                  </select>
                </div>

                {(attributeForm.dataType === 'list' || attributeForm.dataType === 'multiselect') && (
                  <div>
                    <label style={{ display: 'block', fontSize: '0.875rem', fontWeight: '500', color: '#374151', marginBottom: '0.25rem' }}>
                      Options <span style={{ color: '#dc2626' }}>*</span>
                    </label>
                    <p style={{ fontSize: '0.75rem', color: '#6b7280', marginBottom: '0.5rem' }}>
                      Enter one option per line
                    </p>
                    <textarea
                      value={attributeForm.options?.map(opt => opt.label).join('\n') || ''}
                      onChange={(e) => {
                        // Just store the raw text - don't process yet
                        const rawText = e.target.value;
                        const lines = rawText.split('\n');
                        const newOptions = lines.map((line, index) => ({
                          id: `opt-${index}`,
                          label: line,
                          value: line.trim().toLowerCase().replace(/\s+/g, '_')
                        }));
                        setAttributeForm({
                          ...attributeForm,
                          options: newOptions
                        });
                      }}
                      rows={6}
                      style={{
                        width: '100%',
                        padding: '0.5rem 0.75rem',
                        fontSize: '0.875rem',
                        border: '1px solid #d1d5db',
                        borderRadius: '0.375rem',
                        fontFamily: 'monospace',
                        resize: 'vertical'
                      }}
                      placeholder="Red&#10;Blue&#10;Green&#10;Yellow"
                    />
                    {attributeForm.options && attributeForm.options.filter(opt => opt.label.trim()).length > 0 && (
                      <p style={{ fontSize: '0.75rem', color: '#059669', marginTop: '0.5rem' }}>
                        ✓ {attributeForm.options.filter(opt => opt.label.trim()).length} option(s) ready
                      </p>
                    )}
                  </div>
                )}

                <div>
                  <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', cursor: 'pointer' }}>
                    <input
                      type="checkbox"
                      checked={attributeForm.required}
                      onChange={(e) => setAttributeForm({ ...attributeForm, required: e.target.checked })}
                      style={{ width: '1rem', height: '1rem', cursor: 'pointer' }}
                    />
                    <span style={{ fontSize: '0.875rem', fontWeight: '500', color: '#374151' }}>Required Field</span>
                  </label>
                </div>

                <div>
                  <label style={{ display: 'block', fontSize: '0.875rem', fontWeight: '500', color: '#374151', marginBottom: '0.25rem' }}>
                    Description
                  </label>
                  <textarea
                    value={attributeForm.description}
                    onChange={(e) => setAttributeForm({ ...attributeForm, description: e.target.value })}
                    rows={3}
                    style={{
                      width: '100%',
                      padding: '0.375rem 0.75rem',
                      fontSize: '0.875rem',
                      border: '1px solid #d1d5db',
                      borderRadius: '0.375rem',
                      fontFamily: 'inherit'
                    }}
                    placeholder="Optional description..."
                  />
                </div>
              </div>

              <div style={{ display: 'flex', gap: '0.75rem', marginTop: '1rem', paddingTop: '1rem', borderTop: '1px solid #e5e7eb' }}>
                <button
                  onClick={handleSaveAttribute}
                  style={{
                    padding: '0.5rem 1rem',
                    backgroundColor: '#14b8a6',
                    color: 'white',
                    fontSize: '0.875rem',
                    fontWeight: '500',
                    borderRadius: '0.375rem',
                    border: 'none',
                    cursor: 'pointer'
                  }}
                >
                  {editingAttribute ? 'Update' : 'Create'} Attribute
                </button>
                <button
                  onClick={() => setShowAttributeModal(false)}
                  style={{
                    padding: '0.5rem 1rem',
                    backgroundColor: '#f3f4f6',
                    color: '#374151',
                    fontSize: '0.875rem',
                    fontWeight: '500',
                    borderRadius: '0.375rem',
                    border: 'none',
                    cursor: 'pointer'
                  }}
                >
                  Cancel
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Set Create Modal */}
      {showSetModal && (
        <div style={{
          position: 'fixed',
          inset: 0,
          backgroundColor: 'rgba(0, 0, 0, 0.5)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          zIndex: 50,
          padding: '1rem'
        }}>
          <div style={{
            backgroundColor: 'white',
            borderRadius: '0.5rem',
            boxShadow: '0 20px 25px -5px rgb(0 0 0 / 0.1)',
            maxWidth: '42rem',
            width: '100%'
          }}>
            <div style={{ padding: '1rem', borderBottom: '1px solid #e5e7eb', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <h3 style={{ fontSize: '1.125rem', fontWeight: '600', color: '#111827', margin: 0 }}>
                Create Attribute Set
              </h3>
              <button
                onClick={() => setShowSetModal(false)}
                style={{
                  color: '#9ca3af',
                  cursor: 'pointer',
                  background: 'none',
                  border: 'none',
                  fontSize: '1.25rem'
                }}
              >
                <i className="ri-close-line"></i>
              </button>
            </div>
            
            <div style={{ padding: '1rem' }}>
              <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
                <div>
                  <label style={{ display: 'block', fontSize: '0.875rem', fontWeight: '500', color: '#374151', marginBottom: '0.25rem' }}>
                    Set Name <span style={{ color: '#dc2626' }}>*</span>
                  </label>
                  <input
                    type="text"
                    value={setForm.name}
                    onChange={(e) => {
                      const newName = e.target.value;
                      setSetForm({ 
                        ...setForm, 
                        name: newName,
                        // Auto-populate code if it's empty
                        code: setForm.code === '' ? newName.toLowerCase().replace(/\s+/g, '_') : setForm.code
                      });
                    }}
                    style={{
                      width: '100%',
                      padding: '0.375rem 0.75rem',
                      fontSize: '0.875rem',
                      border: '1px solid #d1d5db',
                      borderRadius: '0.375rem'
                    }}
                    placeholder="e.g., Clothing, Electronics"
                  />
                </div>

                <div>
                  <label style={{ display: 'block', fontSize: '0.875rem', fontWeight: '500', color: '#374151', marginBottom: '0.25rem' }}>
                    Set Code <span style={{ color: '#dc2626' }}>*</span>
                  </label>
                  <input
                    type="text"
                    value={setForm.code}
                    onChange={(e) => setSetForm({ ...setForm, code: e.target.value.toLowerCase().replace(/\s+/g, '_') })}
                    style={{
                      width: '100%',
                      padding: '0.375rem 0.75rem',
                      fontSize: '0.875rem',
                      border: '1px solid #d1d5db',
                      borderRadius: '0.375rem',
                      fontFamily: 'monospace'
                    }}
                    placeholder="e.g., clothing, electronics"
                  />
                </div>

                <div>
                  <label style={{ display: 'block', fontSize: '0.875rem', fontWeight: '500', color: '#374151', marginBottom: '0.25rem' }}>
                    Description
                  </label>
                  <textarea
                    value={setForm.description}
                    onChange={(e) => setSetForm({ ...setForm, description: e.target.value })}
                    rows={3}
                    style={{
                      width: '100%',
                      padding: '0.375rem 0.75rem',
                      fontSize: '0.875rem',
                      border: '1px solid #d1d5db',
                      borderRadius: '0.375rem',
                      fontFamily: 'inherit'
                    }}
                    placeholder="Optional description..."
                  />
                </div>
              </div>

              <div style={{ display: 'flex', gap: '0.75rem', marginTop: '1rem', paddingTop: '1rem', borderTop: '1px solid #e5e7eb' }}>
                <button
                  onClick={handleSaveSet}
                  style={{
                    padding: '0.5rem 1rem',
                    backgroundColor: '#14b8a6',
                    color: 'white',
                    fontSize: '0.875rem',
                    fontWeight: '500',
                    borderRadius: '0.375rem',
                    border: 'none',
                    cursor: 'pointer'
                  }}
                >
                  Create Set
                </button>
                <button
                  onClick={() => setShowSetModal(false)}
                  style={{
                    padding: '0.5rem 1rem',
                    backgroundColor: '#f3f4f6',
                    color: '#374151',
                    fontSize: '0.875rem',
                    fontWeight: '500',
                    borderRadius: '0.375rem',
                    border: 'none',
                    cursor: 'pointer'
                  }}
                >
                  Cancel
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Set Edit Modal with Drag-Drop */}
      {showSetEditModal && editingSet && (
        <div style={{
          position: 'fixed',
          inset: 0,
          backgroundColor: 'rgba(0, 0, 0, 0.5)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          zIndex: 50,
          padding: '1rem'
        }}>
          <div style={{
            backgroundColor: 'white',
            borderRadius: '0.5rem',
            boxShadow: '0 20px 25px -5px rgb(0 0 0 / 0.1)',
            maxWidth: '56rem',
            width: '100%',
            maxHeight: '90vh',
            overflowY: 'auto'
          }}>
            <div style={{ padding: '1rem', borderBottom: '1px solid #e5e7eb', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <h3 style={{ fontSize: '1.125rem', fontWeight: '600', color: '#111827', margin: 0 }}>
                Edit Attribute Set: {setForm.name}
              </h3>
              <button
                onClick={async () => {
                  // Save changes before closing
                  if (editingSet) {
                    try {
                      await attributeSetService.update(editingSet.id, setForm);
                      await loadAttributeSets();
                    } catch (error) {
                      console.error('Failed to save changes:', error);
                      alert('Failed to save changes');
                    }
                  }
                  setShowSetEditModal(false);
                }}
                style={{
                  color: '#9ca3af',
                  cursor: 'pointer',
                  background: 'none',
                  border: 'none',
                  fontSize: '1.25rem'
                }}
              >
                <i className="ri-close-line"></i>
              </button>
            </div>
            
            <div style={{ padding: '1rem' }}>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr auto 1fr', gap: '1rem', alignItems: 'start' }}>
                {/* Available Attributes */}
                <div>
                  <h4 style={{ fontSize: '0.875rem', fontWeight: '600', color: '#111827', marginBottom: '0.5rem' }}>
                    Available Attributes
                  </h4>
                  <div style={{ 
                    border: '1px solid #e5e7eb', 
                    borderRadius: '0.375rem', 
                    padding: '0.75rem',
                    minHeight: '400px',
                    maxHeight: '500px',
                    overflowY: 'auto',
                    backgroundColor: '#f9fafb'
                  }}>
                    {availableAttributes.length === 0 ? (
                      <p style={{ textAlign: 'center', color: '#9ca3af', fontSize: '0.875rem', padding: '2rem 0' }}>
                        All attributes added
                      </p>
                    ) : (
                      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
                        {availableAttributes.map(attr => (
                          <div
                            key={attr.id}
                            style={{
                              padding: '0.75rem',
                              backgroundColor: 'white',
                              border: '1px solid #e5e7eb',
                              borderRadius: '0.375rem',
                              display: 'flex',
                              justifyContent: 'space-between',
                              alignItems: 'center'
                            }}
                          >
                            <div style={{ flex: 1 }}>
                              <div style={{ fontSize: '0.875rem', fontWeight: '500', color: '#111827' }}>{attr.name}</div>
                              <div style={{ fontSize: '0.75rem', color: '#6b7280', marginTop: '0.25rem' }}>
                                {getDataTypeLabel(attr.dataType)}
                              </div>
                            </div>
                            <button
                              onClick={() => handleAddAttributeToSet(attr.id)}
                              style={{
                                padding: '0.25rem 0.5rem',
                                backgroundColor: '#14b8a6',
                                color: 'white',
                                border: 'none',
                                borderRadius: '0.25rem',
                                cursor: 'pointer',
                                fontSize: '1rem',
                                fontWeight: 'bold'
                              }}
                              title="Add to set"
                            >
                              &gt;
                            </button>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                </div>

                {/* Middle Buttons */}
                <div style={{ display: 'flex', flexDirection: 'column', justifyContent: 'center', gap: '1rem', paddingTop: '2rem' }}>
                  <button
                    onClick={() => {
                      // Add all
                      availableAttributes.forEach(attr => handleAddAttributeToSet(attr.id));
                    }}
                    style={{
                      padding: '0.5rem 1rem',
                      backgroundColor: '#14b8a6',
                      color: 'white',
                      border: 'none',
                      borderRadius: '0.375rem',
                      cursor: 'pointer',
                      fontSize: '1rem',
                      fontWeight: 'bold'
                    }}
                    title="Add all"
                  >
                    &gt;&gt;
                  </button>
                  <button
                    onClick={() => {
                      // Remove all
                      [...(setForm.attributeIds || [])].forEach(id => handleRemoveAttributeFromSet(id));
                    }}
                    style={{
                      padding: '0.5rem 1rem',
                      backgroundColor: '#6b7280',
                      color: 'white',
                      border: 'none',
                      borderRadius: '0.375rem',
                      cursor: 'pointer',
                      fontSize: '1rem',
                      fontWeight: 'bold'
                    }}
                    title="Remove all"
                  >
                    &lt;&lt;
                  </button>
                </div>

                {/* Selected Attributes */}
                <div>
                  <h4 style={{ fontSize: '0.875rem', fontWeight: '600', color: '#111827', marginBottom: '0.5rem' }}>
                    Set Attributes
                  </h4>
                  <div style={{ 
                    border: '1px solid #e5e7eb', 
                    borderRadius: '0.375rem', 
                    padding: '0.75rem',
                    minHeight: '400px',
                    maxHeight: '500px',
                    overflowY: 'auto',
                    backgroundColor: '#f0fdf4'
                  }}>
                    {setAttributes_selected.length === 0 ? (
                      <p style={{ textAlign: 'center', color: '#9ca3af', fontSize: '0.875rem', padding: '2rem 0' }}>
                        No attributes yet
                      </p>
                    ) : (
                      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
                        {setAttributes_selected.map((attr, index) => (
                          <div
                            key={attr.id}
                            style={{
                              padding: '0.75rem',
                              backgroundColor: 'white',
                              border: '1px solid #e5e7eb',
                              borderRadius: '0.375rem',
                              display: 'flex',
                              justifyContent: 'space-between',
                              alignItems: 'center'
                            }}
                          >
                            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flex: 1 }}>
                              <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem' }}>
                                <button
                                  onClick={() => {
                                    if (index > 0) {
                                      const newIds = [...(setForm.attributeIds || [])];
                                      [newIds[index - 1], newIds[index]] = [newIds[index], newIds[index - 1]];
                                      setSetForm({ ...setForm, attributeIds: newIds });
                                    }
                                  }}
                                  disabled={index === 0}
                                  style={{
                                    padding: '0.125rem 0.25rem',
                                    backgroundColor: index === 0 ? '#e5e7eb' : '#2563eb',
                                    color: 'white',
                                    border: 'none',
                                    borderRadius: '0.25rem',
                                    cursor: index === 0 ? 'not-allowed' : 'pointer',
                                    fontSize: '0.75rem'
                                  }}
                                  title="Move up"
                                >
                                  ▲
                                </button>
                                <button
                                  onClick={() => {
                                    if (index < setAttributes_selected.length - 1) {
                                      const newIds = [...(setForm.attributeIds || [])];
                                      [newIds[index], newIds[index + 1]] = [newIds[index + 1], newIds[index]];
                                      setSetForm({ ...setForm, attributeIds: newIds });
                                    }
                                  }}
                                  disabled={index === setAttributes_selected.length - 1}
                                  style={{
                                    padding: '0.125rem 0.25rem',
                                    backgroundColor: index === setAttributes_selected.length - 1 ? '#e5e7eb' : '#2563eb',
                                    color: 'white',
                                    border: 'none',
                                    borderRadius: '0.25rem',
                                    cursor: index === setAttributes_selected.length - 1 ? 'not-allowed' : 'pointer',
                                    fontSize: '0.75rem'
                                  }}
                                  title="Move down"
                                >
                                  ▼
                                </button>
                              </div>
                              <div style={{ flex: 1 }}>
                                <div style={{ fontSize: '0.875rem', fontWeight: '500', color: '#111827' }}>{attr.name}</div>
                                <div style={{ fontSize: '0.75rem', color: '#6b7280', marginTop: '0.25rem' }}>
                                  {getDataTypeLabel(attr.dataType)}
                                </div>
                              </div>
                            </div>
                            <button
                              onClick={() => handleRemoveAttributeFromSet(attr.id)}
                              style={{
                                padding: '0.25rem 0.5rem',
                                backgroundColor: '#dc2626',
                                color: 'white',
                                border: 'none',
                                borderRadius: '0.25rem',
                                cursor: 'pointer',
                                fontSize: '1rem',
                                fontWeight: 'bold'
                              }}
                              title="Remove from set"
                            >
                              &lt;
                            </button>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                </div>
              </div>
              
              {/* Save Button */}
              <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.75rem', marginTop: '1rem', paddingTop: '1rem', borderTop: '1px solid #e5e7eb' }}>
                <button
                  onClick={async () => {
                    if (editingSet) {
                      try {
                        await attributeSetService.update(editingSet.id, setForm);
                        await loadAttributeSets();
                        setShowSetEditModal(false);
                      } catch (error) {
                        console.error('Failed to save changes:', error);
                        alert('Failed to save changes');
                      }
                    }
                  }}
                  style={{
                    padding: '0.5rem 1rem',
                    backgroundColor: '#14b8a6',
                    color: 'white',
                    fontSize: '0.875rem',
                    fontWeight: '500',
                    borderRadius: '0.375rem',
                    border: 'none',
                    cursor: 'pointer'
                  }}
                >
                  Save Changes
                </button>
                <button
                  onClick={() => setShowSetEditModal(false)}
                  style={{
                    padding: '0.5rem 1rem',
                    backgroundColor: '#f3f4f6',
                    color: '#374151',
                    fontSize: '0.875rem',
                    fontWeight: '500',
                    borderRadius: '0.375rem',
                    border: 'none',
                    cursor: 'pointer'
                  }}
                >
                  Close
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
