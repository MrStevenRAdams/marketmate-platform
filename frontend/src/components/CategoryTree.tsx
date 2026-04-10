import React, { useState } from 'react';

interface Category {
  id: string;
  category_id?: string;
  name: string;
  description?: string;
  parent_id?: string;
  level: number;
  active?: boolean;
  children?: Category[];
}

interface Props {
  categories: Category[];
  onSelect?: (category: Category) => void;
  onEdit?: (category: Category) => void;
  onDelete?: (categoryId: string) => void;
}

export default function CategoryTree({ categories, onSelect, onEdit, onDelete }: Props) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  const toggleExpand = (categoryId: string) => {
    const newExpanded = new Set(expanded);
    if (newExpanded.has(categoryId)) {
      newExpanded.delete(categoryId);
    } else {
      newExpanded.add(categoryId);
    }
    setExpanded(newExpanded);
  };

  const renderCategory = (category: Category, depth: number = 0) => {
    const catId = category.id || category.category_id || '';
    const hasChildren = category.children && category.children.length > 0;
    const isExpanded = expanded.has(catId);
    const indent = depth * 24;

    return (
      <div key={catId}>
        {/* Category Row */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            padding: '12px 16px',
            paddingLeft: `${16 + indent}px`,
            borderBottom: '1px solid var(--border)',
            backgroundColor: 'var(--bg-secondary)',
            cursor: 'pointer',
            transition: 'background-color 0.2s'
          }}
          onMouseEnter={(e) => {
            e.currentTarget.style.backgroundColor = 'var(--bg-tertiary)';
          }}
          onMouseLeave={(e) => {
            e.currentTarget.style.backgroundColor = 'var(--bg-secondary)';
          }}
        >
          {/* Expand/Collapse Icon */}
          <div
            onClick={(e) => {
              e.stopPropagation();
              if (hasChildren) toggleExpand(catId);
            }}
            style={{
              width: '24px',
              height: '24px',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              marginRight: '8px',
              cursor: hasChildren ? 'pointer' : 'default',
              opacity: hasChildren ? 1 : 0.3
            }}
          >
            {hasChildren ? (
              isExpanded ? (
                <i className="ri-arrow-down-s-line" style={{ fontSize: '20px', color: 'var(--text-primary)' }}></i>
              ) : (
                <i className="ri-arrow-right-s-line" style={{ fontSize: '20px', color: 'var(--text-primary)' }}></i>
              )
            ) : (
              <i className="ri-subtract-line" style={{ fontSize: '16px', opacity: 0.3 }}></i>
            )}
          </div>

          {/* Folder Icon */}
          <i
            className={hasChildren ? 'ri-folder-line' : 'ri-file-line'}
            style={{
              fontSize: '18px',
              marginRight: '12px',
              color: hasChildren ? 'var(--primary)' : 'var(--text-muted)'
            }}
          ></i>

          {/* Category Name */}
          <div
            onClick={() => onSelect?.(category)}
            style={{
              flex: 1,
              fontSize: '14px',
              fontWeight: depth === 0 ? '600' : '500',
              color: 'var(--text-primary)'
            }}
          >
            {category.name}
            {category.description && (
              <div style={{
                fontSize: '12px',
                fontWeight: '400',
                color: 'var(--text-muted)',
                marginTop: '2px'
              }}>
                {category.description}
              </div>
            )}
          </div>

          {/* Item Count Badge */}
          {hasChildren && (
            <div
              style={{
                padding: '2px 8px',
                backgroundColor: 'var(--bg-tertiary)',
                border: '1px solid var(--border)',
                borderRadius: '12px',
                fontSize: '11px',
                fontWeight: '600',
                color: 'var(--text-muted)',
                marginRight: '12px'
              }}
            >
              {category.children?.length || 0}
            </div>
          )}

          {/* Status Badge */}
          {category.active !== undefined && (
            <span 
              className={`badge badge-${category.active ? 'success' : 'warning'}`}
              style={{ marginRight: '12px' }}
            >
              {category.active ? 'Active' : 'Inactive'}
            </span>
          )}

          {/* Action Buttons */}
          <div style={{ display: 'flex', gap: '4px' }}>
            {onEdit && (
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  onEdit(category);
                }}
                style={{
                  padding: '6px 12px',
                  backgroundColor: 'transparent',
                  border: '1px solid var(--border)',
                  borderRadius: '4px',
                  color: 'var(--text-primary)',
                  fontSize: '12px',
                  cursor: 'pointer',
                  transition: 'all 0.2s'
                }}
                onMouseEnter={(e) => {
                  e.currentTarget.style.backgroundColor = 'var(--primary-glow)';
                  e.currentTarget.style.borderColor = 'var(--primary)';
                  e.currentTarget.style.color = 'var(--primary)';
                }}
                onMouseLeave={(e) => {
                  e.currentTarget.style.backgroundColor = 'transparent';
                  e.currentTarget.style.borderColor = 'var(--border)';
                  e.currentTarget.style.color = 'var(--text-primary)';
                }}
              >
                <i className="ri-edit-line" style={{ marginRight: '4px' }}></i>
                Edit
              </button>
            )}
            {onDelete && (
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  if (window.confirm(`Delete category "${category.name}"?`)) {
                    onDelete(catId);
                  }
                }}
                style={{
                  padding: '6px 12px',
                  backgroundColor: 'transparent',
                  border: '1px solid var(--danger)',
                  borderRadius: '4px',
                  color: 'var(--danger)',
                  fontSize: '12px',
                  cursor: 'pointer',
                  transition: 'all 0.2s'
                }}
                onMouseEnter={(e) => {
                  e.currentTarget.style.backgroundColor = 'var(--danger)';
                  e.currentTarget.style.color = 'white';
                }}
                onMouseLeave={(e) => {
                  e.currentTarget.style.backgroundColor = 'transparent';
                  e.currentTarget.style.color = 'var(--danger)';
                }}
              >
                <i className="ri-delete-bin-line" style={{ marginRight: '4px' }}></i>
                Delete
              </button>
            )}
          </div>
        </div>

        {/* Children (if expanded) */}
        {hasChildren && isExpanded && (
          <div>
            {category.children!.map(child => renderCategory(child, depth + 1))}
          </div>
        )}
      </div>
    );
  };

  return (
    <div style={{
      border: '1px solid var(--border)',
      borderRadius: '8px',
      overflow: 'hidden',
      backgroundColor: 'var(--bg-primary)'
    }}>
      {categories.length === 0 ? (
        <div style={{
          padding: 'var(--spacing-4xl)',
          textAlign: 'center',
          color: 'var(--text-muted)'
        }}>
          <i className="ri-folder-open-line" style={{ fontSize: '48px', display: 'block', marginBottom: '12px', opacity: 0.3 }}></i>
          <p style={{ fontSize: '14px' }}>No categories yet</p>
        </div>
      ) : (
        categories.map(category => renderCategory(category, 0))
      )}
    </div>
  );
}
