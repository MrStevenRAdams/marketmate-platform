import { useState, useRef, useEffect, useCallback } from 'react';
import {
  ChevronRight, Eye, FolderOpen, Tag, Hash, MapPin, Building2,
  Package, Zap, Trash2, Link, ShoppingCart,
  Truck, DollarSign, XCircle, Layers, Calendar, CalendarDays,
  Printer, FileText, List, ClipboardList, Barcode,
  PlayCircle, FastForward,
  Settings, StickyNote, Code, Scissors, Delete, Ban, FileCode2,
  Cog, ChevronDown,
} from 'lucide-react';

// ─── Types ────────────────────────────────────────────────────────────────────

export interface OrderActionsMenuProps {
  /** IDs of currently selected orders (empty = no selection) */
  selectedOrderIds: string[];
  /** Single order object for per-row actions (optional – bulk if null) */
  singleOrder?: {
    order_id: string;
    external_order_id?: string;
    order_number?: string;
    status?: string;
  } | null;
  onViewOrder: (orderId: string) => void;
  // Organise
  onAssignFolder: (orderIds: string[]) => void;
  onAssignTag: (orderIds: string[]) => void;
  onAssignIdentifier: (orderIds: string[]) => void;
  onMoveToLocation: (orderIds: string[]) => void;
  onMoveToFulfilmentCenter: (orderIds: string[]) => void;
  // Items
  onBatchAssignment: (orderIds: string[]) => void;
  onAutoAssignBatches: (orderIds: string[]) => void;
  onClearBatches: (orderIds: string[]) => void;
  onLinkUnlinkedItems: (orderIds: string[]) => void;
  onAddItemsToPO: (orderIds: string[], mode: 'all' | 'out_of_stock') => void;
  // Shipping
  onChangeService: (orderIds: string[]) => void;
  onGetQuotes: (orderIds: string[]) => void;
  onCancelLabel: (orderIds: string[]) => void;
  onSplitPackaging: (orderId: string) => void;
  onChangeDispatchDate: (orderIds: string[]) => void;
  onChangeDeliveryDates: (orderIds: string[]) => void;
  // Print
  onPrintInvoice: (orderIds: string[]) => void;
  onPrintShippingLabel: (orderId: string) => void;
  onPrintPickList: (orderIds: string[]) => void;
  onPrintPackList: (orderId: string) => void;
  onPrintStockItemLabel: (orderIds: string[]) => void;
  // Process
  onProcessOrder: (orderId: string) => void;
  onBatchProcess: (orderIds: string[]) => void;
  // Other Actions
  onChangeStatus: (orderIds: string[]) => void;
  onViewOrderNotes: (orderId: string) => void;
  onViewOrderXML: (orderId: string) => void;
  onSplitOrder: (orderId: string) => void;
  onDeleteOrder: (orderId: string) => void;
  onCancelOrder: (orderId: string) => void;
  onRunRulesEngine: (orderIds: string[]) => void;
  /** Variant: 'toolbar' renders the primary Actions button; 'row' renders a compact ⋯ button */
  variant?: 'toolbar' | 'row';
  modulesWms: boolean;
  modulesPurchaseOrders: boolean;
}

// ─── Menu data ────────────────────────────────────────────────────────────────

// A menu item can be a leaf action or a sub-menu parent.
type MenuItem =
  | { type: 'action'; label: string; icon: React.ReactNode; action: () => void; danger?: boolean; disabled?: boolean }
  | { type: 'submenu'; label: string; icon: React.ReactNode; children: MenuItem[]; disabled?: boolean }
  | { type: 'separator' };

// ─── Component ────────────────────────────────────────────────────────────────

export default function OrderActionsMenu({
  selectedOrderIds,
  singleOrder,
  onViewOrder,
  onAssignFolder, onAssignTag, onAssignIdentifier, onMoveToLocation, onMoveToFulfilmentCenter,
  onBatchAssignment, onAutoAssignBatches, onClearBatches, onLinkUnlinkedItems, onAddItemsToPO,
  onChangeService, onGetQuotes, onCancelLabel, onSplitPackaging,
  onChangeDispatchDate, onChangeDeliveryDates,
  onPrintInvoice, onPrintShippingLabel, onPrintPickList, onPrintPackList, onPrintStockItemLabel,
  onProcessOrder, onBatchProcess,
  onChangeStatus, onViewOrderNotes, onViewOrderXML, onSplitOrder, onDeleteOrder, onCancelOrder,
  onRunRulesEngine,
  variant = 'toolbar',
  modulesWms,
  modulesPurchaseOrders,
}: OrderActionsMenuProps) {
  const [open, setOpen] = useState(false);
  const [activeSubmenu, setActiveSubmenu] = useState<string | null>(null);
  const [activeSubSubmenu, setActiveSubSubmenu] = useState<string | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  // Effective order IDs: if we have a singleOrder (row variant), use it; otherwise use selection
  const effectiveIds = singleOrder
    ? [singleOrder.order_id]
    : selectedOrderIds;

  const singleId = effectiveIds.length === 1 ? effectiveIds[0] : null;
  const hasSingle = effectiveIds.length === 1;
  const hasAny = effectiveIds.length > 0;

  const closeAll = useCallback(() => {
    setOpen(false);
    setActiveSubmenu(null);
    setActiveSubSubmenu(null);
  }, []);

  // Close on outside click
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        closeAll();
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [open, closeAll]);

  // Close on Escape
  useEffect(() => {
    if (!open) return;
    const handler = (e: KeyboardEvent) => { if (e.key === 'Escape') closeAll(); };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [open, closeAll]);

  const act = (fn: () => void) => {
    fn();
    closeAll();
  };

  // ── Build menu tree ────────────────────────────────────────────────────────

  const menuItems: MenuItem[] = [
    {
      type: 'action',
      label: 'View Order',
      icon: <Eye size={13} />,
      disabled: !hasSingle,
      action: () => act(() => singleId && onViewOrder(singleId)),
    },
    { type: 'separator' },
    {
      type: 'submenu',
      label: 'Organise',
      icon: <FolderOpen size={13} />,
      disabled: !hasAny,
      children: [
        {
          type: 'action', label: 'Folders', icon: <FolderOpen size={12} />,
          action: () => act(() => onAssignFolder(effectiveIds)),
        },
        {
          type: 'action', label: 'Tags', icon: <Tag size={12} />,
          action: () => act(() => onAssignTag(effectiveIds)),
        },
        {
          type: 'action', label: 'Identifiers', icon: <Hash size={12} />,
          action: () => act(() => onAssignIdentifier(effectiveIds)),
        },
        {
          type: 'action', label: 'Move to Location', icon: <MapPin size={12} />,
          action: () => act(() => onMoveToLocation(effectiveIds)),
        },
        {
          type: 'action', label: 'Move to Fulfilment Center', icon: <Building2 size={12} />,
          action: () => act(() => onMoveToFulfilmentCenter(effectiveIds)),
        },
      ],
    },
    {
      type: 'submenu',
      label: 'Items',
      icon: <Package size={13} />,
      disabled: !hasAny,
      children: [
        ...(modulesWms ? [
          {
            type: 'action' as const, label: 'Batch Assignment', icon: <Layers size={12} />,
            action: () => act(() => onBatchAssignment(effectiveIds)),
          },
          {
            type: 'action' as const, label: 'Auto-assign Batches', icon: <Zap size={12} />,
            action: () => act(() => onAutoAssignBatches(effectiveIds)),
          },
          {
            type: 'action' as const, label: 'Clear Batches', icon: <Trash2 size={12} />,
            action: () => act(() => onClearBatches(effectiveIds)),
          },
        ] : []),
        {
          type: 'action' as const, label: 'Link Unlinked Items', icon: <Link size={12} />,
          action: () => act(() => onLinkUnlinkedItems(effectiveIds)),
        },
        ...((modulesWms || modulesPurchaseOrders) ? [{ type: 'separator' as const }] : []),
        ...(modulesPurchaseOrders ? [
          {
            type: 'submenu' as const,
            label: 'Add Items to Purchase Order',
            icon: <ShoppingCart size={12} />,
            children: [
              {
                type: 'action' as const, label: 'All Items', icon: <ClipboardList size={12} />,
                action: () => act(() => onAddItemsToPO(effectiveIds, 'all')),
              },
              {
                type: 'action' as const, label: 'Out of Stock Only', icon: <Ban size={12} />,
                action: () => act(() => onAddItemsToPO(effectiveIds, 'out_of_stock')),
              },
            ],
          },
        ] : []),
      ],
    },
    {
      type: 'submenu',
      label: 'Shipping',
      icon: <Truck size={13} />,
      disabled: !hasAny,
      children: [
        {
          type: 'action', label: 'Change Service', icon: <Truck size={12} />,
          action: () => act(() => onChangeService(effectiveIds)),
        },
        {
          type: 'action', label: 'Get Quotes', icon: <DollarSign size={12} />,
          action: () => act(() => onGetQuotes(effectiveIds)),
        },
        {
          type: 'action', label: 'Cancel Label', icon: <XCircle size={12} />,
          action: () => act(() => onCancelLabel(effectiveIds)),
        },
        {
          type: 'action', label: 'Split Packaging', icon: <Layers size={12} />,
          disabled: !hasSingle,
          action: () => act(() => singleId && onSplitPackaging(singleId)),
        },
        { type: 'separator' },
        {
          type: 'action', label: 'Change Dispatch Date', icon: <Calendar size={12} />,
          action: () => act(() => onChangeDispatchDate(effectiveIds)),
        },
        {
          type: 'action', label: 'Change Delivery Dates', icon: <CalendarDays size={12} />,
          action: () => act(() => onChangeDeliveryDates(effectiveIds)),
        },
      ],
    },
    {
      type: 'submenu',
      label: 'Print',
      icon: <Printer size={13} />,
      disabled: !hasAny,
      children: [
        {
          type: 'action', label: 'Invoice', icon: <FileText size={12} />,
          action: () => act(() => onPrintInvoice(effectiveIds)),
        },
        {
          type: 'action', label: 'Shipping Label', icon: <Barcode size={12} />,
          disabled: !hasSingle,
          action: () => act(() => singleId && onPrintShippingLabel(singleId)),
        },
        {
          type: 'action', label: 'Pick List', icon: <List size={12} />,
          action: () => act(() => onPrintPickList(effectiveIds)),
        },
        {
          type: 'action', label: 'Pack List', icon: <ClipboardList size={12} />,
          disabled: !hasSingle,
          action: () => act(() => singleId && onPrintPackList(singleId)),
        },
        {
          type: 'action', label: 'Stock Item Label', icon: <Barcode size={12} />,
          action: () => act(() => onPrintStockItemLabel(effectiveIds)),
        },
      ],
    },
    {
      type: 'submenu',
      label: 'Process Order',
      icon: <PlayCircle size={13} />,
      disabled: !hasAny,
      children: [
        {
          type: 'action', label: 'Process', icon: <PlayCircle size={12} />,
          disabled: !hasSingle,
          action: () => act(() => singleId && onProcessOrder(singleId)),
        },
        {
          type: 'action', label: 'Batch Process', icon: <FastForward size={12} />,
          action: () => act(() => onBatchProcess(effectiveIds)),
        },
      ],
    },
    {
      type: 'submenu',
      label: 'Other Actions',
      icon: <Settings size={13} />,
      disabled: !hasAny,
      children: [
        {
          type: 'action', label: 'Change Status', icon: <Cog size={12} />,
          action: () => act(() => onChangeStatus(effectiveIds)),
        },
        { type: 'separator' },
        {
          type: 'action', label: 'View Order Notes', icon: <StickyNote size={12} />,
          disabled: !hasSingle,
          action: () => act(() => singleId && onViewOrderNotes(singleId)),
        },
        {
          type: 'action', label: 'View Order XML', icon: <Code size={12} />,
          disabled: !hasSingle,
          action: () => act(() => singleId && onViewOrderXML(singleId)),
        },
        { type: 'separator' },
        {
          type: 'action', label: 'Split Order', icon: <Scissors size={12} />,
          disabled: !hasSingle,
          action: () => act(() => singleId && onSplitOrder(singleId)),
        },
        {
          type: 'action', label: 'Delete Order', icon: <Delete size={12} />,
          disabled: !hasSingle,
          danger: true,
          action: () => act(() => singleId && onDeleteOrder(singleId)),
        },
        {
          type: 'action', label: 'Cancel Order', icon: <Ban size={12} />,
          disabled: !hasSingle,
          danger: true,
          action: () => act(() => singleId && onCancelOrder(singleId)),
        },
        { type: 'separator' },
        {
          type: 'action', label: 'Run Rules Engine', icon: <FileCode2 size={12} />,
          action: () => act(() => onRunRulesEngine(effectiveIds)),
        },
      ],
    },
  ];

  // ── Render ─────────────────────────────────────────────────────────────────

  const renderMenuItems = (items: MenuItem[], depth: number = 0) =>
    items.map((item, idx) => {
      if (item.type === 'separator') {
        return <div key={`sep-${depth}-${idx}`} className="oam-separator" />;
      }

      const key = `${depth}-${idx}-${item.label}`;

      if (item.type === 'submenu') {
        const submenuKey = `${depth}-${item.label}`;
        const isActive = depth === 0 ? activeSubmenu === submenuKey : activeSubSubmenu === submenuKey;

        return (
          <div
            key={key}
            className={`oam-item oam-item-sub ${item.disabled ? 'oam-item-disabled' : ''} ${isActive ? 'oam-item-active' : ''}`}
            onMouseEnter={() => {
              if (item.disabled) return;
              if (depth === 0) {
                setActiveSubmenu(submenuKey);
                setActiveSubSubmenu(null);
              } else {
                setActiveSubSubmenu(submenuKey);
              }
            }}
          >
            <span className="oam-item-icon">{item.icon}</span>
            <span className="oam-item-label">{item.label}</span>
            <ChevronRight size={11} className="oam-chevron" />
            {isActive && !item.disabled && (
              <div className={`oam-submenu oam-submenu-depth-${depth}`}>
                {renderMenuItems(item.children, depth + 1)}
              </div>
            )}
          </div>
        );
      }

      // type === 'action'
      return (
        <button
          key={key}
          className={`oam-item oam-item-action ${item.disabled ? 'oam-item-disabled' : ''} ${item.danger ? 'oam-item-danger' : ''}`}
          onClick={item.disabled ? undefined : item.action}
          disabled={item.disabled}
          onMouseEnter={() => {
            if (depth === 0) setActiveSubmenu(null);
            else if (depth === 1) setActiveSubSubmenu(null);
          }}
        >
          <span className="oam-item-icon">{item.icon}</span>
          <span className="oam-item-label">{item.label}</span>
        </button>
      );
    });

  const isToolbar = variant === 'toolbar';

  return (
    <div className={`oam-container ${isToolbar ? 'oam-toolbar' : 'oam-row'}`} ref={containerRef}>
      {isToolbar ? (
        <button
          className={`btn-sec oam-trigger-toolbar ${open ? 'oam-trigger-open' : ''}`}
          onClick={() => setOpen(v => !v)}
          title={hasAny ? `Actions (${effectiveIds.length} selected)` : 'Select orders to use Actions'}
        >
          Actions
          <ChevronDown size={12} style={{ transition: 'transform 0.15s', transform: open ? 'rotate(180deg)' : 'none' }} />
        </button>
      ) : (
        <button
          className={`btn-sm oam-trigger-row ${open ? 'oam-trigger-open' : ''}`}
          onClick={e => { e.stopPropagation(); setOpen(v => !v); }}
          title="Order actions"
          style={{ fontSize: 11, padding: '3px 7px' }}
        >
          ···
        </button>
      )}

      {open && (
        <div
          className={`oam-menu ${isToolbar ? 'oam-menu-toolbar' : 'oam-menu-row'}`}
          onMouseLeave={() => { setActiveSubmenu(null); setActiveSubSubmenu(null); }}
        >
          {renderMenuItems(menuItems)}
        </div>
      )}
    </div>
  );
}
