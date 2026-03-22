import { useState, useEffect, useRef } from 'react';
import { categoryService, fileService } from '../services/api';

interface Category {
  category_id: string;
  name: string;
  parent_id: string | null;
  image_url?: string;
  images?: Array<{url: string, path: string, sort_order: number} | string>;
  description?: string;
  attribute_set?: string;
  created_at: string;
  updated_at: string;
  children?: Category[];
}

const DEFAULT_CATEGORY: Category = {
  category_id: 'default-root',
  name: 'Default',
  parent_id: null,
  description: 'Default root category',
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
  children: []
};

export default function CategoryList() {
  const [categories, setCategories] = useState<Category[]>([DEFAULT_CATEGORY]);
  const [selectedCategory, setSelectedCategory] = useState<Category | null>(DEFAULT_CATEGORY);
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set(['default-root']));
  const [isCreating, setIsCreating] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const [uploadMethod, setUploadMethod] = useState<'url' | 'file'>('file');
  const [isDraggingFile, setIsDraggingFile] = useState(false);
  const [isUploadingImage, setIsUploadingImage] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);
  
  const [formData, setFormData] = useState({
    name: '',
    parent_id: null as string | null,
    image_url: '',
    description: '',
    attribute_set: ''
  });

  useEffect(() => {
    loadCategories();
  }, []);

  const buildCategoryTree = (flatList: Category[]): Category[] => {
    if (!flatList || flatList.length === 0) return [];
    
    const byId: Record<string, Category> = {};
    const roots: Category[] = [];

    flatList.forEach(cat => {
      byId[cat.category_id] = { ...cat, children: [] };
    });

    flatList.forEach(cat => {
      const node = byId[cat.category_id];
      if (cat.parent_id && byId[cat.parent_id]) {
        byId[cat.parent_id].children!.push(node);
      } else {
        roots.push(node);
      }
    });

    return roots;
  };

  async function loadCategories() {
    setIsLoading(true);
    try {
      console.log('Loading categories...');
      const response = await categoryService.list();
      console.log('Categories response:', response);
      
      let categoryData: Category[] = [];
      if (Array.isArray(response.data)) {
        categoryData = response.data;
      } else if (response.data?.data && Array.isArray(response.data.data)) {
        categoryData = response.data.data;
      }

      console.log('Parsed category data:', categoryData);

      if (categoryData.length === 0) {
        // No categories - show only default
        setCategories([DEFAULT_CATEGORY]);
        setSelectedCategory(DEFAULT_CATEGORY);
        setExpandedIds(new Set(['default-root']));
      } else {
        // Build tree and add as children of default
        const tree = buildCategoryTree(categoryData);
        const rootWithDefault = {
          ...DEFAULT_CATEGORY,
          children: tree
        };
        setCategories([rootWithDefault]);
        // Keep default expanded to show children
        setExpandedIds(prev => new Set([...prev, 'default-root']));
      }
    } catch (error) {
      console.error('Failed to load categories:', error);
      setCategories([DEFAULT_CATEGORY]);
      setSelectedCategory(DEFAULT_CATEGORY);
    } finally {
      setIsLoading(false);
    }
  };

  const handleToggle = (categoryId: string, e: React.MouseEvent) => {
    e.stopPropagation();
    setExpandedIds(prev => {
      const next = new Set(prev);
      if (next.has(categoryId)) {
        next.delete(categoryId);
      } else {
        next.add(categoryId);
      }
      return next;
    });
  };

  const handleSelect = (category: Category) => {
    setSelectedCategory(category);
    setIsCreating(false);
    setIsEditing(false);
  };

  const handleCreateNew = () => {
    setIsCreating(true);
    setIsEditing(false);
    setUploadMethod('file');
    setFormData({
      name: '',
      parent_id: selectedCategory?.category_id === 'default-root' ? null : selectedCategory?.category_id || null,
      image_url: '',
      description: '',
      attribute_set: ''
    });
  };

  const handleEdit = () => {
    if (!selectedCategory || selectedCategory.category_id === 'default-root') return;
    
    setIsEditing(true);
    setIsCreating(false);
    setUploadMethod('file');
    
    // Extract image URL from images array if it exists
    let imageUrl = '';
    if (selectedCategory.image_url) {
      imageUrl = selectedCategory.image_url;
    } else if (selectedCategory.images && Array.isArray(selectedCategory.images) && selectedCategory.images.length > 0) {
      // Backend stores as array of {url, path, sort_order}
      const firstImage = selectedCategory.images[0];
      imageUrl = typeof firstImage === 'string' ? firstImage : firstImage.url;
    }
    
    setFormData({
      name: selectedCategory.name,
      parent_id: selectedCategory.parent_id,
      image_url: imageUrl,
      description: selectedCategory.description || '',
      attribute_set: selectedCategory.attribute_set || ''
    });
  };

  const handleDelete = async () => {
    if (!selectedCategory || selectedCategory.category_id === 'default-root') return;
    
    // Count children recursively
    const countChildren = (cat: Category): number => {
      let count = 0;
      if (cat.children && cat.children.length > 0) {
        count = cat.children.length;
        cat.children.forEach(child => {
          count += countChildren(child);
        });
      }
      return count;
    };
    
    const childCount = countChildren(selectedCategory);
    
    let confirmMessage = `Are you sure you want to delete "${selectedCategory.name}"?`;
    if (childCount > 0) {
      confirmMessage = `⚠️ Warning: Deleting "${selectedCategory.name}" will also delete ${childCount} subcategory/subcategories.\n\nAre you sure you want to continue?`;
    }
    
    if (!confirm(confirmMessage)) return;
    
    try {
      console.log('Deleting category:', selectedCategory.category_id);
      await categoryService.delete(selectedCategory.category_id);
      console.log('Delete successful');
      await loadCategories();
      setSelectedCategory(DEFAULT_CATEGORY);
    } catch (error) {
      console.error('Failed to delete category:', error);
      alert('Failed to delete category');
    }
  };

  const handleFileUpload = async (file: File) => {
    if (!file.type.startsWith('image/')) {
      alert('Please upload an image file');
      return;
    }

    if (file.size > 5 * 1024 * 1024) {
      alert('Image size must be less than 5MB');
      return;
    }

    setIsUploadingImage(true);
    try {
      const formDataUpload = new FormData();
      formDataUpload.append('file', file);
      formDataUpload.append('entity_type', 'categories');
      formDataUpload.append('entity_id', 'new-category'); // Placeholder ID for new categories
      formDataUpload.append('sub_folder', 'images');

      console.log('Uploading image...');
      const response = await fileService.upload(formDataUpload);
      console.log('Upload response:', response);
      console.log('Response data:', response.data);
      console.log('Response data.data:', response.data?.data);

      // Backend returns { data: { data: { url: "..." } } }
      const uploadData = response.data?.data || response.data;
      
      if (uploadData?.url) {
        setFormData(prev => ({ ...prev, image_url: uploadData.url }));
        console.log('Image uploaded successfully:', uploadData.url);
      } else {
        console.error('No URL found in response:', response);
        throw new Error('No URL in response');
      }
    } catch (error) {
      console.error('Failed to upload image:', error);
      alert('Failed to upload image. Make sure GCS is configured.');
    } finally {
      setIsUploadingImage(false);
    }
  };

  const handleFileDragOver = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setIsDraggingFile(true);
  };

  const handleFileDragLeave = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setIsDraggingFile(false);
  };

  const handleFileDrop = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setIsDraggingFile(false);

    const files = e.dataTransfer.files;
    if (files.length > 0) {
      handleFileUpload(files[0]);
    }
  };

  const handleFileInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files;
    if (files && files.length > 0) {
      handleFileUpload(files[0]);
    }
  };

  const handleRemoveImage = () => {
    setFormData(prev => ({ ...prev, image_url: '' }));
    if (fileInputRef.current) {
      fileInputRef.current.value = '';
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    
    try {
      console.log('Submitting category:', formData);
      
      const categoryData: any = {
        name: formData.name,
        parent_id: formData.parent_id || null,
        description: formData.description || null
      };

      // Add images array if image URL exists
      if (formData.image_url) {
        categoryData.images = [{
          url: formData.image_url,
          path: formData.image_url,
          sort_order: 0
        }];
      }

      console.log('Category data being sent:', categoryData);

      if (isEditing && selectedCategory && selectedCategory.category_id !== 'default-root') {
        console.log('Updating category:', selectedCategory.category_id);
        const response = await categoryService.update(selectedCategory.category_id, categoryData);
        console.log('Update response:', response);
      } else {
        console.log('Creating new category');
        const response = await categoryService.create(categoryData);
        console.log('Create response:', response);
        
        // Auto-expand parent category to show new child
        if (formData.parent_id) {
          setExpandedIds(prev => new Set([...prev, formData.parent_id!]));
        } else {
          // Expand default root to show new root-level category
          setExpandedIds(prev => new Set([...prev, 'default-root']));
        }
      }

      setIsCreating(false);
      setIsEditing(false);
      await loadCategories();
    } catch (error: any) {
      console.error('Failed to save category:', error);
      console.error('Error response:', error.response);
      alert(`Failed to save category: ${error.response?.data?.error || error.message}`);
    }
  };

  const handleCancel = () => {
    setIsCreating(false);
    setIsEditing(false);
  };

  const renderTreeNode = (category: Category, level = 0): React.ReactNode => {
    const isExpanded = expandedIds.has(category.category_id);
    const hasChildren = category.children && category.children.length > 0;
    const isSelected = selectedCategory?.category_id === category.category_id;
    
    // Level colors for visual hierarchy
    const levelColors = [
      '#1e40af', // Level 0 - Dark blue
      '#059669', // Level 1 - Green
      '#d97706', // Level 2 - Orange
      '#dc2626', // Level 3 - Red
      '#7c3aed', // Level 4+ - Purple
    ];
    const levelColor = levelColors[Math.min(level, levelColors.length - 1)];

    return (
      <div key={category.category_id} style={{ position: 'relative' }}>
        {/* Connecting line to parent */}
        {level > 0 && (
          <div
            style={{
              position: 'absolute',
              left: `${level * 20 - 8}px`,
              top: 0,
              width: '20px',
              height: '50%',
              borderLeft: `2px solid ${levelColor}`,
              borderBottom: `2px solid ${levelColor}`,
              opacity: 0.3
            }}
          />
        )}
        
        <div
          className={`flex items-center gap-2 px-3 py-2 cursor-pointer hover:bg-gray-50 ${
            isSelected ? 'bg-blue-50 border-l-4 border-blue-600' : 'border-l-4 border-transparent'
          }`}
          style={{ 
            paddingLeft: `${level * 20 + 12}px`,
            position: 'relative',
            backgroundColor: isSelected ? '#eff6ff' : 'transparent'
          }}
          onClick={() => handleSelect(category)}
        >
          {hasChildren ? (
            <button
              onClick={(e) => handleToggle(category.category_id, e)}
              style={{ 
                width: '24px',
                height: '24px',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                border: '1px solid #d1d5db',
                backgroundColor: '#f3f4f6',
                borderRadius: '4px',
                cursor: 'pointer',
                zIndex: 1,
                transition: 'all 0.2s'
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.backgroundColor = levelColor;
                const icon = e.currentTarget.querySelector('i');
                if (icon) (icon as HTMLElement).style.color = 'white';
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.backgroundColor = '#f3f4f6';
                const icon = e.currentTarget.querySelector('i');
                if (icon) (icon as HTMLElement).style.color = levelColor;
              }}
            >
              <i 
                className={`ri-arrow-${isExpanded ? 'down' : 'right'}-s-line`}
                style={{ color: levelColor, fontSize: '16px', fontWeight: 'bold' }}
              ></i>
            </button>
          ) : (
            <span style={{ width: '24px' }}></span>
          )}
          <i 
            className="ri-folder-line" 
            style={{ color: levelColor, fontSize: '1.1rem' }}
          ></i>
          <span 
            className="flex-1 text-sm" 
            style={{ 
              color: isSelected ? levelColor : '#111827',
              fontWeight: isSelected ? '600' : '500'
            }}
          >
            {category.name}
          </span>
          {hasChildren && (
            <span 
              className="text-xs px-2 py-0.5 rounded-full" 
              style={{ 
                color: 'white',
                backgroundColor: levelColor,
                fontWeight: '600'
              }}
            >
              {category.children!.length}
            </span>
          )}
        </div>
        {hasChildren && isExpanded && (
          <div style={{ position: 'relative' }}>
            {/* Vertical line for children */}
            {category.children!.length > 1 && (
              <div
                style={{
                  position: 'absolute',
                  left: `${(level + 1) * 20 - 8}px`,
                  top: 0,
                  bottom: 0,
                  width: '2px',
                  backgroundColor: levelColor,
                  opacity: 0.3
                }}
              />
            )}
            {category.children!.map(child => renderTreeNode(child, level + 1))}
          </div>
        )}
      </div>
    );
  };

  return (
    <div style={{ minHeight: '100vh', backgroundColor: '#f9fafb' }}>
      {/* Header */}
      <div style={{ 
        backgroundColor: 'white', 
        borderBottom: '1px solid #e5e7eb', 
        padding: '1rem 1.5rem' 
      }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div>
            <h1 style={{ fontSize: '1.5rem', fontWeight: 'bold', color: '#111827', margin: 0 }}>
              Categories
            </h1>
            <p style={{ fontSize: '0.875rem', color: '#6b7280', marginTop: '0.25rem' }}>
              Manage your product categories
            </p>
          </div>
          <button
            onClick={handleCreateNew}
            style={{
              padding: '0.5rem 1rem',
              backgroundColor: '#2563eb',
              color: 'white',
              borderRadius: '0.5rem',
              border: 'none',
              cursor: 'pointer',
              display: 'flex',
              alignItems: 'center',
              gap: '0.5rem',
              fontSize: '0.875rem',
              fontWeight: '500'
            }}
            onMouseOver={(e) => e.currentTarget.style.backgroundColor = '#1d4ed8'}
            onMouseOut={(e) => e.currentTarget.style.backgroundColor = '#2563eb'}
          >
            <i className="ri-add-line"></i>
            Create New Category
          </button>
        </div>
      </div>

      {/* Main Content */}
      <div style={{ 
        display: 'flex', 
        gap: '1.5rem', 
        padding: '1.5rem',
        height: 'calc(100vh - 120px)'
      }}>
        {/* Left: Tree View */}
        <div style={{ 
          width: '33.333%', 
          backgroundColor: 'white', 
          borderRadius: '0.5rem', 
          boxShadow: '0 1px 3px 0 rgb(0 0 0 / 0.1)',
          border: '1px solid #e5e7eb',
          overflow: 'auto'
        }}>
          <div style={{ 
            padding: '1rem', 
            borderBottom: '1px solid #e5e7eb',
            backgroundColor: '#f9fafb'
          }}>
            <h2 style={{ 
              fontWeight: '600', 
              color: '#111827', 
              margin: 0,
              fontSize: '1rem'
            }}>
              Category Tree
            </h2>
          </div>
          <div style={{ padding: '0.5rem 0' }}>
            {isLoading ? (
              <div style={{ padding: '2rem', textAlign: 'center', color: '#6b7280' }}>
                Loading...
              </div>
            ) : (
              categories.map(cat => renderTreeNode(cat))
            )}
          </div>
        </div>

        {/* Right: Details/Form */}
        <div style={{ 
          flex: 1, 
          backgroundColor: 'white', 
          borderRadius: '0.5rem', 
          boxShadow: '0 1px 3px 0 rgb(0 0 0 / 0.1)',
          border: '1px solid #e5e7eb',
          overflow: 'auto'
        }}>
          <div style={{ padding: '1.5rem' }}>
            {isCreating || isEditing ? (
              // Form
              <div>
                <h2 style={{ 
                  fontSize: '1.25rem', 
                  fontWeight: 'bold', 
                  color: '#111827', 
                  marginBottom: '1.5rem' 
                }}>
                  {isCreating ? 'Create New Category' : 'Edit Category'}
                </h2>
                <form onSubmit={handleSubmit}>
                  <div style={{ marginBottom: '1.5rem' }}>
                    <label style={{ 
                      display: 'block', 
                      fontSize: '0.875rem', 
                      fontWeight: '500', 
                      color: '#374151',
                      marginBottom: '0.5rem'
                    }}>
                      Category Name *
                    </label>
                    <input
                      type="text"
                      required
                      value={formData.name}
                      onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                      style={{
                        width: '100%',
                        padding: '0.5rem 0.75rem',
                        border: '1px solid #d1d5db',
                        borderRadius: '0.5rem',
                        fontSize: '0.875rem'
                      }}
                      placeholder="Enter category name"
                    />
                  </div>

                  {/* Image Upload Section */}
                  <div style={{ marginBottom: '1.5rem' }}>
                    <label style={{ 
                      display: 'block', 
                      fontSize: '0.875rem', 
                      fontWeight: '500', 
                      color: '#374151',
                      marginBottom: '0.5rem'
                    }}>
                      Category Image
                    </label>
                    
                    {/* Upload Method Toggle */}
                    <div style={{ display: 'flex', gap: '0.5rem', marginBottom: '0.75rem' }}>
                      <button
                        type="button"
                        onClick={() => setUploadMethod('file')}
                        style={{
                          padding: '0.25rem 0.75rem',
                          fontSize: '0.75rem',
                          border: '1px solid #d1d5db',
                          borderRadius: '0.375rem',
                          backgroundColor: uploadMethod === 'file' ? '#2563eb' : 'white',
                          color: uploadMethod === 'file' ? 'white' : '#374151',
                          cursor: 'pointer'
                        }}
                      >
                        Upload File
                      </button>
                      <button
                        type="button"
                        onClick={() => setUploadMethod('url')}
                        style={{
                          padding: '0.25rem 0.75rem',
                          fontSize: '0.75rem',
                          border: '1px solid #d1d5db',
                          borderRadius: '0.375rem',
                          backgroundColor: uploadMethod === 'url' ? '#2563eb' : 'white',
                          color: uploadMethod === 'url' ? 'white' : '#374151',
                          cursor: 'pointer'
                        }}
                      >
                        Image URL
                      </button>
                    </div>

                    {uploadMethod === 'file' ? (
                      <div>
                        {/* Drag & Drop Zone - Also acts as click zone */}
                        <div
                          onDragOver={handleFileDragOver}
                          onDragLeave={handleFileDragLeave}
                          onDrop={handleFileDrop}
                          onClick={() => fileInputRef.current?.click()}
                          style={{
                            border: `2px dashed ${isDraggingFile ? '#2563eb' : '#d1d5db'}`,
                            borderRadius: '0.5rem',
                            padding: '2rem',
                            textAlign: 'center',
                            cursor: 'pointer',
                            backgroundColor: isDraggingFile ? '#eff6ff' : '#f9fafb',
                            marginBottom: '0.75rem'
                          }}
                        >
                          {isUploadingImage ? (
                            <>
                              <i className="ri-loader-4-line animate-spin" style={{ fontSize: '2rem', color: '#2563eb' }}></i>
                              <p style={{ margin: '0.5rem 0', fontSize: '0.875rem', color: '#2563eb' }}>
                                Uploading image...
                              </p>
                            </>
                          ) : (
                            <>
                              <i className="ri-upload-cloud-2-line" style={{ fontSize: '2rem', color: '#9ca3af' }}></i>
                              <p style={{ margin: '0.5rem 0', fontSize: '0.875rem', color: '#6b7280' }}>
                                Drag and drop an image, or click to browse
                              </p>
                              <p style={{ margin: 0, fontSize: '0.75rem', color: '#9ca3af' }}>
                                PNG, JPG up to 5MB
                              </p>
                            </>
                          )}
                        </div>
                        <input
                          ref={fileInputRef}
                          type="file"
                          accept="image/*"
                          onChange={handleFileInputChange}
                          style={{ display: 'none' }}
                        />
                      </div>
                    ) : (
                      <input
                        type="url"
                        value={formData.image_url}
                        onChange={(e) => setFormData({ ...formData, image_url: e.target.value })}
                        style={{
                          width: '100%',
                          padding: '0.5rem 0.75rem',
                          border: '1px solid #d1d5db',
                          borderRadius: '0.5rem',
                          fontSize: '0.875rem'
                        }}
                        placeholder="https://example.com/image.jpg"
                      />
                    )}

                    {/* Image Preview */}
                    {formData.image_url && !isUploadingImage && (
                      <div style={{ marginTop: '0.75rem', position: 'relative', display: 'inline-block' }}>
                        <img
                          src={formData.image_url}
                          alt="Preview"
                          style={{
                            maxWidth: '200px',
                            maxHeight: '200px',
                            borderRadius: '0.5rem',
                            border: '1px solid #e5e7eb'
                          }}
                          onError={(e) => {
                            console.error('Image failed to load:', formData.image_url);
                            alert('Image failed to load. Please check the URL.');
                          }}
                        />
                        <button
                          type="button"
                          onClick={handleRemoveImage}
                          style={{
                            position: 'absolute',
                            top: '-8px',
                            right: '-8px',
                            backgroundColor: '#ef4444',
                            color: 'white',
                            border: 'none',
                            borderRadius: '50%',
                            width: '24px',
                            height: '24px',
                            cursor: 'pointer',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            fontSize: '16px',
                            fontWeight: 'bold'
                          }}
                        >
                          ×
                        </button>
                      </div>
                    )}
                  </div>

                  <div style={{ marginBottom: '1.5rem' }}>
                    <label style={{ 
                      display: 'block', 
                      fontSize: '0.875rem', 
                      fontWeight: '500', 
                      color: '#374151',
                      marginBottom: '0.5rem'
                    }}>
                      Description
                    </label>
                    <textarea
                      value={formData.description}
                      onChange={(e) => setFormData({ ...formData, description: e.target.value })}
                      rows={4}
                      style={{
                        width: '100%',
                        padding: '0.5rem 0.75rem',
                        border: '1px solid #d1d5db',
                        borderRadius: '0.5rem',
                        fontSize: '0.875rem',
                        fontFamily: 'inherit'
                      }}
                      placeholder="Enter category description"
                    />
                  </div>

                  <div style={{ marginBottom: '1.5rem' }}>
                    <label style={{ 
                      display: 'block', 
                      fontSize: '0.875rem', 
                      fontWeight: '500', 
                      color: '#374151',
                      marginBottom: '0.5rem'
                    }}>
                      Attribute Set
                    </label>
                    <input
                      type="text"
                      value={formData.attribute_set}
                      onChange={(e) => setFormData({ ...formData, attribute_set: e.target.value })}
                      style={{
                        width: '100%',
                        padding: '0.5rem 0.75rem',
                        border: '1px solid #d1d5db',
                        borderRadius: '0.5rem',
                        fontSize: '0.875rem'
                      }}
                      placeholder="Optional attribute set name"
                    />
                  </div>

                  <div style={{ 
                    display: 'flex', 
                    gap: '0.75rem', 
                    paddingTop: '1rem',
                    borderTop: '1px solid #e5e7eb'
                  }}>
                    <button
                      type="submit"
                      disabled={isUploadingImage}
                      style={{
                        padding: '0.5rem 1rem',
                        backgroundColor: isUploadingImage ? '#9ca3af' : '#2563eb',
                        color: 'white',
                        borderRadius: '0.5rem',
                        border: 'none',
                        cursor: isUploadingImage ? 'not-allowed' : 'pointer',
                        fontSize: '0.875rem',
                        fontWeight: '500'
                      }}
                    >
                      {isCreating ? 'Create Category' : 'Update Category'}
                    </button>
                    <button
                      type="button"
                      onClick={handleCancel}
                      disabled={isUploadingImage}
                      style={{
                        padding: '0.5rem 1rem',
                        backgroundColor: '#e5e7eb',
                        color: '#374151',
                        borderRadius: '0.5rem',
                        border: 'none',
                        cursor: isUploadingImage ? 'not-allowed' : 'pointer',
                        fontSize: '0.875rem',
                        fontWeight: '500'
                      }}
                    >
                      Cancel
                    </button>
                  </div>
                </form>
              </div>
            ) : selectedCategory ? (
              // Details View
              <div>
                <div style={{ 
                  display: 'flex', 
                  alignItems: 'center', 
                  justifyContent: 'space-between',
                  marginBottom: '1.5rem'
                }}>
                  <h2 style={{ 
                    fontSize: '1.25rem', 
                    fontWeight: 'bold', 
                    color: '#111827',
                    margin: 0
                  }}>
                    {selectedCategory.name}
                  </h2>
                  {selectedCategory.category_id !== 'default-root' && (
                    <div style={{ display: 'flex', gap: '0.5rem' }}>
                      <button
                        onClick={handleEdit}
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
                        onClick={handleDelete}
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
                  )}
                </div>

                {selectedCategory.image_url && (
                  <div style={{ marginBottom: '1rem' }}>
                    <img
                      src={selectedCategory.image_url}
                      alt={selectedCategory.name}
                      style={{
                        maxWidth: '100%',
                        height: 'auto',
                        borderRadius: '0.5rem',
                        border: '1px solid #e5e7eb',
                        maxHeight: '400px'
                      }}
                    />
                  </div>
                )}

                {selectedCategory.description && (
                  <div style={{ marginBottom: '1rem' }}>
                    <h3 style={{ 
                      fontSize: '0.875rem', 
                      fontWeight: '500', 
                      color: '#374151',
                      marginBottom: '0.5rem'
                    }}>
                      Description
                    </h3>
                    <p style={{ color: '#4b5563', fontSize: '0.875rem', margin: 0 }}>
                      {selectedCategory.description}
                    </p>
                  </div>
                )}

                {selectedCategory.attribute_set && (
                  <div style={{ marginBottom: '1rem' }}>
                    <h3 style={{ 
                      fontSize: '0.875rem', 
                      fontWeight: '500', 
                      color: '#374151',
                      marginBottom: '0.5rem'
                    }}>
                      Attribute Set
                    </h3>
                    <p style={{ color: '#4b5563', fontSize: '0.875rem', margin: 0 }}>
                      {selectedCategory.attribute_set}
                    </p>
                  </div>
                )}

                {selectedCategory.category_id !== 'default-root' && (
                  <div style={{ 
                    display: 'grid', 
                    gridTemplateColumns: '1fr 1fr',
                    gap: '1rem',
                    fontSize: '0.875rem',
                    paddingTop: '1rem',
                    borderTop: '1px solid #e5e7eb'
                  }}>
                    <div>
                      <span style={{ color: '#6b7280' }}>Created:</span>
                      <p style={{ color: '#111827', margin: '0.25rem 0 0 0' }}>
                        {new Date(selectedCategory.created_at).toLocaleString()}
                      </p>
                    </div>
                    <div>
                      <span style={{ color: '#6b7280' }}>Updated:</span>
                      <p style={{ color: '#111827', margin: '0.25rem 0 0 0' }}>
                        {new Date(selectedCategory.updated_at).toLocaleString()}
                      </p>
                    </div>
                  </div>
                )}
              </div>
            ) : (
              // Empty State
              <div style={{ 
                textAlign: 'center', 
                paddingTop: '3rem', 
                paddingBottom: '3rem',
                color: '#6b7280'
              }}>
                <i className="ri-folder-open-line" style={{ fontSize: '3rem', marginBottom: '1rem', display: 'block' }}></i>
                <p style={{ margin: 0 }}>Select a category to view details</p>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
