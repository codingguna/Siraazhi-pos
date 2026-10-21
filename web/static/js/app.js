/* ==========================================================================
   Siraazhi Pro POS — Main Application Controller
   
   WHAT IT IS:
     The primary Single Page Application (SPA) controller running inside the browser.
     It connects all UI actions to the Go REST API, controls the POS shopping cart,
     calculates real-time GST splits, captures hardware barcode scanner inputs,
     and enforces the Master Host vs Staff Mobile Terminal security model.

   HOW IT WORKS:
     1. Role Detection (Master Host vs Staff Mobile Terminal):
        - When accessed via localhost (127.0.0.1 or host machine), it boots in Master Mode,
          granting full access to all tabs (Billing, Stock, Production, Purchases,
          Expenses, Sales Ledger, GSTR-1, Settings).
        - When accessed from a smartphone/tablet over Wi-Fi (http://<ip>:8080), it boots
          in Staff Billing Mode, restricting the UI to the POS Billing screen only.
        - Staff cannot view expenses, delete bills, change prices, or export accounting data.
        - The shop owner can unlock Master Mode on any mobile device by entering the Master PIN.
     2. Cart Management & Live Calculations:
        - Maintains in-memory CART array.
        - Checks inventory availability before adding items.
        - Real-time tax prorating and cash rounding upon quantity or discount adjustments.
     3. Barcode Scanner Support:
        - Automatically captures rapid keystrokes from USB/Bluetooth handheld barcode scanners.
        - Performs an instant lookup on the barcode column and adds matched SKUs directly to cart.
     4. Universal Multi-Printer Routing:
        - Formats finalized invoices for 80mm thermal roll, 58mm portable slip, or A4 tax invoice.
        - Native vector PDF download button for WhatsApp / email sharing.

   DATA FLOW:
     User Action / Barcode Scan -> In-Memory Cart State -> REST API Call ->
     SQLite Database Transaction -> Hardware Print / PDF Trigger -> UI Refresh
   ========================================================================== */

// Global State
let SETTINGS = {};
let PRODUCTS = [];
let ALL_BILLS = [];
let ACTIVE_CATEGORY = 'all';
let IS_MASTER_MODE = true; // Determined dynamically below
const DEFAULT_MASTER_PIN = '1234';
let LAST_BILL = null;

// Multi-Bill State Management (Hold & Resume Active Carts)
let BILL_TABS = [
  {
    id: 1,
    title: 'Bill 1',
    cart: [],
    customer: { name: '', phone: '', gstin: '', address: '' },
    gstOn: false,
    discount: 0,
    paid: '',
    payMode: 'Cash'
  }
];
let ACTIVE_BILL_INDEX = 0;
let CART = BILL_TABS[0].cart;

/**
 * api is a generic async fetch wrapper with JSON serialization and error handling.
 * Flow: attaches headers & admin token -> dispatches fetch -> parses JSON -> throws on HTTP error.
 */
async function api(method, endpoint, body) {
  const headers = { 'Content-Type': 'application/json' };
  const adminToken = localStorage.getItem('siraazhi_admin_token');
  if (adminToken) {
    headers['X-Admin-Token'] = adminToken;
  }

  const res = await fetch(endpoint, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined
  });

  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data.error || `HTTP Error ${res.status}`);
  }
  return data;
}

const $ = id => document.getElementById(id);
const formatINR = n => '₹' + Number(n || 0).toLocaleString('en-IN', {
  minimumFractionDigits: 2,
  maximumFractionDigits: 2
});
const escapeHtml = str => String(str || '').replace(/[&<>'"]/g, tag => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;' }[tag] || tag));

/**
 * initApp bootstraps the application on page load.
 * Flow: initializes dates -> determines Master vs Staff role -> fetches settings & products ->
 *       renders product grid -> binds keyboard shortcuts -> loads dashboard if in master mode.
 */
async function initApp() {
  $('billDate').value = new Date().toISOString().slice(0, 10);
  $('puDate').value = new Date().toISOString().slice(0, 10);
  $('exDate').value = new Date().toISOString().slice(0, 10);
  $('prodBatchDate').value = new Date().toISOString().slice(0, 10);

  // Set default dates for reports (first day of month to today)
  const today = new Date().toISOString().slice(0, 10);
  const firstOfMonth = today.slice(0, 8) + '01';
  $('filterFrom').value = firstOfMonth;
  $('filterTo').value = today;

  setupKeyboardShortcuts();

  try {
    SETTINGS = await api('GET', '/api/settings');
    PRODUCTS = await api('GET', '/api/products');
    
    // Determine Master vs Staff mode with up-to-date settings & PIN
    determineRoleMode();

    // Apply and synchronize default settings to UI and billing counter
    applySettingsToUI();

    renderProductsGrid();
    renderCart();
    renderBillTabs();

    if (IS_MASTER_MODE) {
      loadDashboard();
      loadInventoryTable();
      loadProductionTable();
      loadStockMoves();
      loadPurchases();
      loadExpenses();
      loadBills();
      loadGSTR1Report();
      populateSettingsForm();
      switchTab('dash');
    } else {
      switchTab('pos');
    }
  } catch (err) {
    console.error('Initialization error:', err);
  }
}

// --------------------------------------------------------------------------
// 1. Master System vs Client Mobile Terminal Role Detection
// --------------------------------------------------------------------------

/**
 * determineRoleMode checks if caller is on host desktop or a remote Wi-Fi device.
 * Flow: checks window.location.hostname -> if localhost or PIN verified -> Master Mode;
 *       otherwise -> Staff Mobile Billing Mode.
 */
function determineRoleMode() {
  const isLocalHost = window.location.hostname === 'localhost' || 
                      window.location.hostname === '127.0.0.1' || 
                      window.location.protocol === 'file:';
  
  const savedToken = localStorage.getItem('siraazhi_admin_token');
  const validPin = (SETTINGS && SETTINGS.masterPin) ? SETTINGS.masterPin : DEFAULT_MASTER_PIN;

  // Master mode if running locally on host PC or valid unlock token present
  if (isLocalHost || (savedToken && (savedToken === validPin || savedToken === DEFAULT_MASTER_PIN))) {
    enableMasterMode();
  } else {
    enableClientStaffMode();
  }
}

/**
 * enableClientStaffMode hides all non-billing tabs for mobile Wi-Fi devices.
 */
function enableClientStaffMode() {
  IS_MASTER_MODE = false;
  document.body.classList.add('client-staff-mode');

  // Hide all non-billing tabs in staff mobile mode
  const allowedTabs = ['pos'];
  document.querySelectorAll('#navTabs button').forEach(btn => {
    if (!allowedTabs.includes(btn.dataset.tab)) {
      btn.style.display = 'none';
    }
  });

  // Update header indicator
  $('statusText').textContent = 'Staff Billing Mode';
  $('statusPill').style.background = '#0284c7';

  // Mobile/Client terminals print to Master System
  if ($('saveAndPrintBtn')) {
    $('saveAndPrintBtn').innerHTML = '🖨️ Send to Master Printer (F10)';
  }
  if ($('printQueueBtn')) {
    $('printQueueBtn').style.display = 'none';
  }

  // Add an unlock master button
  const tools = document.querySelector('.header-tools');
  if (!$('unlockMasterBtn')) {
    const unlockBtn = document.createElement('button');
    unlockBtn.id = 'unlockMasterBtn';
    unlockBtn.className = 'tool-btn';
    unlockBtn.innerHTML = '🔑 Unlock Master';
    unlockBtn.onclick = promptMasterPin;
    tools.prepend(unlockBtn);
  }

  switchTab('pos');
}

/**
 * enableMasterMode reveals all management tabs for the host administrator.
 */
function enableMasterMode() {
  IS_MASTER_MODE = true;
  document.body.classList.remove('client-staff-mode');
  
  document.querySelectorAll('#navTabs button').forEach(btn => {
    btn.style.display = 'inline-block';
  });

  $('statusText').textContent = 'Master System';
  $('statusPill').style.background = 'rgba(0, 0, 0, 0.2)';

  if ($('saveAndPrintBtn')) {
    $('saveAndPrintBtn').innerHTML = '🖨 Save & Print (F10)';
  }
  if ($('printQueueBtn')) {
    $('printQueueBtn').style.display = 'inline-flex';
  }

  if ($('unlockMasterBtn')) {
    $('unlockMasterBtn').remove();
  }

  startMasterPrintQueuePoller();
}

/**
 * promptMasterPin prompts the user for admin PIN to upgrade to master mode.
 */
function promptMasterPin() {
  const pin = prompt('Enter Master Admin PIN to unlock full management & reports:');
  if (!pin) return;
  const validPin = (SETTINGS && SETTINGS.masterPin) ? SETTINGS.masterPin : DEFAULT_MASTER_PIN;
  if (pin === validPin) {
    localStorage.setItem('siraazhi_admin_token', pin);
    alert('✅ Master Access Granted.');
    location.reload();
  } else {
    alert('❌ Invalid PIN. Client billing mode remains active.');
  }
}

// --------------------------------------------------------------------------
// 2. Navigation Tabs
// --------------------------------------------------------------------------

document.querySelectorAll('#navTabs button').forEach(btn => {
  btn.onclick = () => {
    switchTab(btn.dataset.tab);
  };
});

/**
 * switchTab activates the chosen tab pane and triggers its data loader.
 */
function switchTab(tabId) {
  if (!IS_MASTER_MODE && tabId !== 'pos') {
    alert('Restricted: Management & reports are only accessible on the Master Host.');
    return;
  }

  document.querySelectorAll('#navTabs button').forEach(b => b.classList.remove('active'));
  document.querySelectorAll('.tab-pane').forEach(p => p.classList.remove('active'));

  const targetBtn = document.querySelector(`#navTabs button[data-tab="${tabId}"]`);
  const targetPane = $(`tab-${tabId}`);

  if (targetBtn) targetBtn.classList.add('active');
  if (targetPane) targetPane.classList.add('active');

  // Trigger data refreshes on tab activation
  if (tabId === 'pos') {
    if (CART.length === 0) {
      applySettingsToUI();
    }
    setTimeout(() => $('barcodeScanInput').focus(), 100);
  } else if (tabId === 'dash') {
    loadDashboard();
  } else if (tabId === 'inventory') {
    loadInventoryTable();
  } else if (tabId === 'production') {
    loadProductionTable();
    loadStockMoves();
  } else if (tabId === 'purchases') {
    loadPurchases();
  } else if (tabId === 'expenses') {
    loadExpenses();
  } else if (tabId === 'bills') {
    loadBills();
  } else if (tabId === 'gstr1') {
    loadGSTR1Report();
  } else if (tabId === 'settings') {
    populateSettingsForm();
    loadInstalledPrintersList();
  }
}

// --------------------------------------------------------------------------
// 3. POS Billing & Catalog
// --------------------------------------------------------------------------

/**
 * renderProductsGrid renders interactive cards for all oil products.
 * Flow: filters by active category and search term -> formats card markup -> injects into grid container.
 */
function renderProductsGrid() {
  const container = $('productsGrid');
  const term = ($('barcodeScanInput').value || '').trim().toLowerCase();

  const filtered = PRODUCTS.filter(p => {
    const matchesCat = ACTIVE_CATEGORY === 'all' || p.category.toLowerCase() === ACTIVE_CATEGORY.toLowerCase();
    const matchesSearch = !term || 
      p.nameEn.toLowerCase().includes(term) || 
      (p.nameTa && p.nameTa.toLowerCase().includes(term)) ||
      (p.barcode && p.barcode.includes(term)) ||
      (p.hsn && p.hsn.includes(term));
    return matchesCat && matchesSearch;
  });

  if (!filtered.length) {
    container.innerHTML = `<div style="grid-column:1/-1; text-align:center; padding:40px; color:#64748b;">No products found matching criteria.</div>`;
    return;
  }

  container.innerHTML = filtered.map(p => {
    const isLow = p.stock <= p.lowAlert;
    return `
      <div class="product-card" onclick="addToCart('${p.id}')">
        <span class="prod-badge ${isLow ? 'low' : ''}">${isLow ? 'Low: ' : 'Stk: '}${p.stock}</span>
        <div>
          <div class="prod-title-en">${p.nameEn}</div>
          <div class="prod-title-ta">${p.nameTa || ''}</div>
          <div class="prod-variant">${p.variant || ''}</div>
        </div>
        <div class="prod-footer">
          <div>
            <div class="prod-price">${formatINR(p.price)}</div>
            ${p.mrp > p.price ? `<div class="prod-mrp">${formatINR(p.mrp)}</div>` : ''}
          </div>
          <div class="prod-stock-tag">${p.unit}</div>
        </div>
      </div>
    `;
  }).join('');
}

// Category filter pills
document.querySelectorAll('#categoryPills .pill-btn').forEach(btn => {
  btn.onclick = () => {
    document.querySelectorAll('#categoryPills .pill-btn').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    ACTIVE_CATEGORY = btn.dataset.cat;
    renderProductsGrid();
  };
});

// Barcode scanner & search input
$('barcodeScanInput').addEventListener('input', () => {
  const code = $('barcodeScanInput').value.trim();
  // Check exact barcode match
  const exactMatch = PRODUCTS.find(p => p.barcode && p.barcode === code);
  if (exactMatch) {
    addToCart(exactMatch.id);
    clearSearch();
    return;
  }
  renderProductsGrid();
});

$('barcodeScanInput').addEventListener('keydown', (e) => {
  if (e.key === 'Enter') {
    const term = $('barcodeScanInput').value.trim();
    const match = PRODUCTS.find(p => 
      (p.barcode && p.barcode === term) || 
      p.nameEn.toLowerCase() === term.toLowerCase()
    );
    if (match) {
      addToCart(match.id);
      clearSearch();
    }
  }
});

function clearSearch() {
  $('barcodeScanInput').value = '';
  renderProductsGrid();
  $('barcodeScanInput').focus();
}

/**
 * addToCart adds an item to the active billing cart with stock availability guard.
 */
function addToCart(productId) {
  const product = PRODUCTS.find(p => p.id === productId);
  if (!product) return;

  if (product.stock <= 0) {
    alert(`⚠️ Warning: ${product.nameEn} has ZERO stock. Please add stock before billing.`);
    return;
  }

  const existing = CART.find(c => c.productId === productId);
  if (existing) {
    if (existing.qty + 1 > product.stock) {
      alert(`Cannot add more: Only ${product.stock} units available in stock.`);
      return;
    }
    existing.qty += 1;
  } else {
    CART.push({
      productId: product.id,
      nameEn: product.nameEn,
      nameTa: product.nameTa,
      hsn: product.hsn,
      barcode: product.barcode,
      price: product.price,
      gstPct: product.gstPct,
      qty: 1
    });
  }

  renderCart();
}

/**
 * updateCartQty adjusts the quantity of a cart line item by +1 or -1.
 */
function updateCartQty(index, delta) {
  if (!CART[index]) return;
  const p = PRODUCTS.find(prod => prod.id === CART[index].productId);
  const newQty = CART[index].qty + delta;

  if (newQty <= 0) {
    CART.splice(index, 1);
  } else {
    if (p && newQty > p.stock) {
      alert(`Stock limit reached: Only ${p.stock} units available.`);
      return;
    }
    CART[index].qty = newQty;
  }
  renderCart();
}

/**
 * setCartQtyDirect handles direct numerical input in the cart table.
 */
function setCartQtyDirect(index, val) {
  const q = Number(val) || 1;
  const p = PRODUCTS.find(prod => prod.id === CART[index].productId);
  if (q <= 0) {
    CART.splice(index, 1);
  } else if (p && q > p.stock) {
    alert(`Only ${p.stock} units in stock. Setting to maximum available.`);
    CART[index].qty = p.stock;
  } else {
    CART[index].qty = q;
  }
  renderCart();
}

function removeFromCart(index) {
  CART.splice(index, 1);
  renderCart();
}

function clearCart() {
  const isGstDefault = SETTINGS && SETTINGS.gstDefaultOn === true;
  if (BILL_TABS[ACTIVE_BILL_INDEX]) {
    BILL_TABS[ACTIVE_BILL_INDEX].cart = [];
    BILL_TABS[ACTIVE_BILL_INDEX].customer = { name: '', phone: '', gstin: '', address: '' };
    BILL_TABS[ACTIVE_BILL_INDEX].discount = 0;
    BILL_TABS[ACTIVE_BILL_INDEX].paid = '';
    BILL_TABS[ACTIVE_BILL_INDEX].gstOn = isGstDefault;
  }
  CART = BILL_TABS[ACTIVE_BILL_INDEX].cart;
  if ($('billDiscount')) $('billDiscount').value = 0;
  if ($('custName')) $('custName').value = '';
  if ($('custPhone')) $('custPhone').value = '';
  if ($('custGstin')) $('custGstin').value = '';
  if ($('custAddress')) $('custAddress').value = '';
  if ($('billPaid')) $('billPaid').value = '';
  if ($('gstToggle')) {
    $('gstToggle').checked = isGstDefault;
    toggleGSTMode();
  }
  renderCart();
  renderBillTabs();
}

/**
 * Multi-Bill Active Tabs Logic (Hold & Resume Active Carts)
 */
function renderBillTabs() {
  const container = $('billTabsList');
  if (!container) return;

  container.innerHTML = BILL_TABS.map((tab, idx) => {
    const isActive = idx === ACTIVE_BILL_INDEX;
    const count = tab.cart.reduce((sum, it) => sum + (it.qty || 1), 0);
    const cust = tab.customer && tab.customer.name ? tab.customer.name : '';
    const label = cust ? `${tab.title} (${cust})` : tab.title;

    return `
      <div class="bill-tab ${isActive ? 'active' : ''}" onclick="switchBillTab(${idx})">
        <span>🧾</span>
        <span>${escapeHtml(label)}</span>
        <span class="tab-badge">${count}</span>
        ${BILL_TABS.length > 1 ? `<span class="tab-close" onclick="closeBillTab(event, ${idx})" title="Close bill">✕</span>` : ''}
      </div>
    `;
  }).join('');

  const titleElem = $('currentBillTitleDisplay');
  if (titleElem && BILL_TABS[ACTIVE_BILL_INDEX]) {
    titleElem.textContent = `${BILL_TABS[ACTIVE_BILL_INDEX].title} / நடப்பு பில்`;
  }
}

function addNewBillTab() {
  saveCurrentBillTabForm();
  const nextId = BILL_TABS.length > 0 ? Math.max(...BILL_TABS.map(b => b.id)) + 1 : 1;
  const isGstDefault = SETTINGS && SETTINGS.gstDefaultOn === true;
  const newBill = {
    id: nextId,
    title: `Bill ${nextId}`,
    cart: [],
    customer: { name: '', phone: '', gstin: '', address: '' },
    gstOn: isGstDefault,
    discount: 0,
    paid: '',
    payMode: 'Cash'
  };
  BILL_TABS.push(newBill);
  ACTIVE_BILL_INDEX = BILL_TABS.length - 1;
  CART = BILL_TABS[ACTIVE_BILL_INDEX].cart;
  loadActiveBillTabForm();
  renderBillTabs();
  renderCart();
  if ($('custName')) $('custName').focus();
}

function switchBillTab(index) {
  if (index === ACTIVE_BILL_INDEX || !BILL_TABS[index]) return;
  saveCurrentBillTabForm();
  ACTIVE_BILL_INDEX = index;
  CART = BILL_TABS[ACTIVE_BILL_INDEX].cart;
  loadActiveBillTabForm();
  renderBillTabs();
  renderCart();
}

function closeBillTab(event, index) {
  if (event) event.stopPropagation();
  if (BILL_TABS.length <= 1) {
    if (BILL_TABS[0].cart.length > 0) {
      if (!confirm('Discard items in this bill?')) return;
    }
    clearCart();
    return;
  }
  if (BILL_TABS[index].cart.length > 0) {
    if (!confirm(`Discard ${BILL_TABS[index].title} with ${BILL_TABS[index].cart.length} items?`)) return;
  }
  BILL_TABS.splice(index, 1);
  if (ACTIVE_BILL_INDEX >= BILL_TABS.length) {
    ACTIVE_BILL_INDEX = BILL_TABS.length - 1;
  }
  CART = BILL_TABS[ACTIVE_BILL_INDEX].cart;
  loadActiveBillTabForm();
  renderBillTabs();
  renderCart();
}

function saveCurrentBillTabForm() {
  if (!BILL_TABS[ACTIVE_BILL_INDEX]) return;
  const b = BILL_TABS[ACTIVE_BILL_INDEX];
  b.customer = {
    name: $('custName') ? $('custName').value.trim() : '',
    phone: $('custPhone') ? $('custPhone').value.trim() : '',
    gstin: $('custGstin') ? $('custGstin').value.trim() : '',
    address: $('custAddress') ? $('custAddress').value.trim() : ''
  };
  b.discount = $('billDiscount') ? Number($('billDiscount').value) || 0 : 0;
  b.paid = $('billPaid') ? $('billPaid').value : '';
  b.payMode = $('billPayMode') ? $('billPayMode').value : 'Cash';
  b.gstOn = $('gstToggle') ? $('gstToggle').checked : true;
}

function loadActiveBillTabForm() {
  if (!BILL_TABS[ACTIVE_BILL_INDEX]) return;
  const b = BILL_TABS[ACTIVE_BILL_INDEX];
  if ($('custName')) $('custName').value = b.customer.name || '';
  if ($('custPhone')) $('custPhone').value = b.customer.phone || '';
  if ($('custGstin')) $('custGstin').value = b.customer.gstin || '';
  if ($('custAddress')) $('custAddress').value = b.customer.address || '';
  if ($('billDiscount')) $('billDiscount').value = b.discount || 0;
  if ($('billPaid')) $('billPaid').value = b.paid || '';
  if ($('billPayMode')) $('billPayMode').value = b.payMode || 'Cash';
  if ($('gstToggle')) {
    const isGstDefault = SETTINGS && SETTINGS.gstDefaultOn === true;
    const tabGst = (b.gstOn !== undefined) ? b.gstOn : isGstDefault;
    $('gstToggle').checked = tabGst;
    toggleGSTMode();
  }
}

/**
 * Instant Customer Database Search & Autocomplete
 */
let customerSearchTimeout = null;
let CUSTOMER_SUGGESTIONS = [];

function onCustomerSearchInput(field) {
  const query = (field === 'phone' ? $('custPhone').value : $('custName').value).trim();
  if (query.length < 1) {
    closeCustomerDropdown();
    return;
  }

  clearTimeout(customerSearchTimeout);
  customerSearchTimeout = setTimeout(async () => {
    try {
      const results = await api('GET', `/api/customers?q=${encodeURIComponent(query)}&limit=8`);
      renderCustomerDropdown(results);
    } catch (err) {
      console.error('Customer search error:', err);
    }
  }, 120);
}

function onCustomerSearchFocus(field) {
  const query = (field === 'phone' ? $('custPhone').value : $('custName').value).trim();
  if (query.length >= 1) {
    onCustomerSearchInput(field);
  }
}

function renderCustomerDropdown(customers) {
  const dropdown = $('customerDropdown');
  const list = $('customerDropdownList');
  if (!dropdown || !list) return;

  if (!customers || customers.length === 0) {
    list.innerHTML = `<div class="customer-empty-sug">⚡ New Customer (will be saved to DB automatically)</div>`;
    dropdown.style.display = 'block';
    return;
  }

  CUSTOMER_SUGGESTIONS = customers;
  list.innerHTML = customers.map((c, i) => `
    <div class="customer-suggestion-item" onclick="selectCustomerByIndex(${i})">
      <div class="cust-sug-left">
        <div class="cust-sug-name"><b>${escapeHtml(c.name)}</b></div>
        <div class="cust-sug-phone">📞 ${escapeHtml(c.phone || '-')}</div>
      </div>
      <div class="cust-sug-right">
        <span class="cust-sug-tag">${c.gstin ? 'B2B' : 'Retail'}</span>
        ${c.gstin ? `<span class="cust-sug-gst">${escapeHtml(c.gstin)}</span>` : ''}
      </div>
    </div>
  `).join('');

  dropdown.style.display = 'block';
}

function selectCustomerByIndex(index) {
  const c = CUSTOMER_SUGGESTIONS[index];
  if (!c) return;

  $('custName').value = c.name || '';
  $('custPhone').value = c.phone || '';
  if (c.gstin) {
    $('custGstin').value = c.gstin;
    $('gstDetailsRow').style.display = 'flex';
  }
  if (c.address) {
    $('custAddress').value = c.address;
    $('gstDetailsRow').style.display = 'flex';
  }

  saveCurrentBillTabForm();
  renderBillTabs();
  closeCustomerDropdown();
}

function closeCustomerDropdown() {
  const dropdown = $('customerDropdown');
  if (dropdown) dropdown.style.display = 'none';
}

// Global click outside to dismiss customer dropdown
document.addEventListener('click', (e) => {
  const container = $('customerBoxContainer');
  if (container && !container.contains(e.target)) {
    closeCustomerDropdown();
  }
});

/**
 * toggleGSTMode switches between Tax Invoice (with GST breakdown) and Cash Bill.
 */
function toggleGSTMode() {
  const isGst = $('gstToggle') ? $('gstToggle').checked : false;
  if (BILL_TABS && BILL_TABS[ACTIVE_BILL_INDEX]) {
    BILL_TABS[ACTIVE_BILL_INDEX].gstOn = isGst;
  }
  if ($('gstToggleLabel')) {
    $('gstToggleLabel').innerHTML = isGst ? 
      '<b>GST Invoice</b> (Tax split)' : 
      '<b>Cash Bill</b> (No tax)';
  }
  if ($('gstDetailsRow')) $('gstDetailsRow').style.display = isGst ? 'flex' : 'none';
  if ($('taxBreakdownRow')) $('taxBreakdownRow').style.display = isGst ? 'flex' : 'none';
  recalcCart();
}

/**
 * renderCart updates the cart table DOM with current items.
 */
function renderCart() {
  const tbody = $('cartTableBody');
  if (!CART.length) {
    tbody.innerHTML = `<tr class="empty-cart-row"><td colspan="5">Cart is empty. Click an item or scan barcode to add.</td></tr>`;
    recalcCart();
    renderBillTabs();
    return;
  }

  tbody.innerHTML = CART.map((it, i) => `
    <tr>
      <td>
        <b>${it.nameEn}</b>
        <div style="font-size:11px; color:#64748b;">${it.nameTa || ''}</div>
      </td>
      <td>
        <div class="qty-control">
          <button class="qty-btn" onclick="updateCartQty(${i}, -1)">-</button>
          <input type="number" class="qty-input" min="1" value="${it.qty}" onchange="setCartQtyDirect(${i}, this.value)">
          <button class="qty-btn" onclick="updateCartQty(${i}, 1)">+</button>
        </div>
      </td>
      <td>${formatINR(it.price)}</td>
      <td style="font-weight:700;">${formatINR(it.qty * it.price)}</td>
      <td>
        <button class="del-btn" onclick="removeFromCart(${i})">✕</button>
      </td>
    </tr>
  `).join('');

  recalcCart();
  renderBillTabs();
  updateMobileFloatingCart();
}

/**
 * recalcCart computes subtotal, taxes, discount, and grand total.
 */
function recalcCart() {
  let subtotal = 0;
  let taxPortion = 0;
  const isGst = $('gstToggle').checked;

  CART.forEach(it => {
    const line = it.qty * it.price;
    subtotal += line;
    if (isGst && it.gstPct > 0) {
      taxPortion += (line * it.gstPct) / (100 + it.gstPct);
    }
  });

  const discount = Math.max(0, Number($('billDiscount').value) || 0);
  const grandTotal = Math.max(0, subtotal - discount);

  $('sumSubtotal').textContent = formatINR(subtotal);
  $('sumTaxAmt').textContent = formatINR(taxPortion);
  $('sumGrandTotal').textContent = formatINR(grandTotal);

  if (!$('billPaid').value) {
    $('billPaid').placeholder = grandTotal.toFixed(0);
  }
}

/**
 * checkoutBill commits the invoice to backend and sends to printer / PDF.
 * Validates COMPULSORY Customer Name and Mobile Phone Number.
 */
async function checkoutBill(printFormat) {
  if (!CART.length) {
    alert('Please add at least one item to the cart.');
    return;
  }

  const custName = ($('custName').value || '').trim();
  const custPhone = ($('custPhone').value || '').trim();

  // Validate compulsory customer details
  if (!custName) {
    alert('⚠️ Customer Name is COMPULSORY. Please enter customer name.');
    $('custName').focus();
    return;
  }
  if (!custPhone) {
    alert('⚠️ Customer Mobile Phone Number is COMPULSORY. Please enter mobile number.');
    $('custPhone').focus();
    return;
  }

  const isGst = $('gstToggle').checked;
  const grandTotal = Math.max(0, CART.reduce((s, it) => s + (it.qty * it.price), 0) - Number($('billDiscount').value || 0));
  const paidVal = $('billPaid').value ? Number($('billPaid').value) : grandTotal;

  const payload = {
    date: $('billDate').value || new Date().toISOString().slice(0, 10),
    customer: {
      name: custName,
      phone: custPhone,
      address: ($('custAddress').value || '').trim(),
      gstin: ($('custGstin').value || '').trim(),
      state: 'Tamil Nadu'
    },
    items: CART,
    discount: Number($('billDiscount').value) || 0,
    gstOn: isGst,
    payMode: $('billPayMode').value || 'Cash',
    paid: paidVal,
    note: ''
  };

  const activeTab = BILL_TABS[ACTIVE_BILL_INDEX];
  const isEditing = activeTab && activeTab.editingBillId;

  try {
    let bill;
    if (isEditing) {
      bill = await api('PUT', `/api/bills/${activeTab.editingBillId}`, payload);
      alert(`✅ Bill ${bill.billNo} updated successfully!`);
    } else {
      bill = await api('POST', '/api/bills', payload);
    }
    
    // Refresh product stock cache
    PRODUCTS = await api('GET', '/api/products');
    renderProductsGrid();

    LAST_BILL = bill;

    // Print or PDF Export
    if (printFormat === 'pdf') {
      PDFEngine.saveBillPDF(bill, SETTINGS);
    } else if (IS_MASTER_MODE) {
      if (printFormat === 'a4' || printFormat === 'a5' || !SETTINGS.printerInterface || SETTINGS.printerInterface === 'browser') {
        PrintEngine.print(bill, printFormat, SETTINGS);
      } else {
        await printBillDirect(bill, printFormat);
      }
    } else {
      // Remote Mobile / Tablet Terminal Mode:
      // Send print job to Master System Print Queue!
      try {
        await api('POST', '/api/print-queue', {
          billId: bill.id,
          bill: bill,
          format: printFormat || (SETTINGS && SETTINGS.defaultPrinter) || '80mm'
        });
        showToast(`✅ Bill ${bill.billNo} Created! Queued to Master Thermal Printer.`, 'success', 5000);
      } catch (qErr) {
        console.error('Queue error:', qErr);
        showToast(`✅ Bill ${bill.billNo} created! (Master spooler note: ${qErr.message})`, 'info');
      }
    }

    // Advance Multi-Bill Tabs: close current tab if multiple bills were open, or clear it
    if (BILL_TABS.length > 1) {
      BILL_TABS.splice(ACTIVE_BILL_INDEX, 1);
      if (ACTIVE_BILL_INDEX >= BILL_TABS.length) {
        ACTIVE_BILL_INDEX = BILL_TABS.length - 1;
      }
      CART = BILL_TABS[ACTIVE_BILL_INDEX].cart;
      loadActiveBillTabForm();
      renderBillTabs();
      renderCart();
    } else {
      clearCart();
    }

    if (IS_MASTER_MODE) {
      loadDashboard();
      loadBills();
    }
  } catch (err) {
    alert('❌ Bill Creation Failed: ' + err.message);
  }
}

// --------------------------------------------------------------------------
// 4. Keyboard Shortcuts for High-Speed Counter Operations
// --------------------------------------------------------------------------
function setupKeyboardShortcuts() {
  window.addEventListener('keydown', (e) => {
    if (e.key === 'F2') {
      e.preventDefault();
      switchTab('pos');
      $('barcodeScanInput').focus();
    } else if (e.key === 'F3') {
      e.preventDefault();
      switchTab('pos');
      $('barcodeScanInput').focus();
      $('barcodeScanInput').select();
    } else if (e.key === 'F4') {
      e.preventDefault();
      $('gstToggle').checked = !$('gstToggle').checked;
      toggleGSTMode();
    } else if (e.key === 'F8' || (e.ctrlKey && e.key.toLowerCase() === 'n')) {
      e.preventDefault();
      switchTab('pos');
      addNewBillTab();
    } else if (e.key === 'F10') {
      e.preventDefault();
      checkoutBill('default');
    } else if (e.key === 'Escape') {
      closeCustomerDropdown();
      closeAllModals();
    }
  });
}

// --------------------------------------------------------------------------
// 5. Dashboard Data Loader
// --------------------------------------------------------------------------
async function loadDashboard() {
  if (!IS_MASTER_MODE) return;
  try {
    const data = await api('GET', '/api/dashboard');
    $('kpiTodaySales').textContent = formatINR(data.todaySales);
    $('kpiTodayCount').textContent = `${data.todayBillsCount} bills issued`;
    $('kpiMonthSales').textContent = formatINR(data.monthSales);
    $('kpiMonthCount').textContent = `${data.monthBillsCount} bills issued`;
    $('kpiStockValue').textContent = formatINR(data.stockValue);
    $('kpiProdCount').textContent = `${data.totalProducts} Catalog SKUs`;
    $('kpiMonthPurchases').textContent = formatINR(data.monthPurchases);
    $('kpiMonthExpenses').textContent = `Overheads: ${formatINR(data.monthExpenses)}`;

    // Low stock container
    const lowContainer = $('lowStockContainer');
    if (data.lowStockItems && data.lowStockItems.length) {
      lowContainer.innerHTML = data.lowStockItems.map(p => `
        <div style="display:flex; justify-content:space-between; padding:8px 0; border-bottom:1px solid #f1f5f9;">
          <div>
            <b>${p.nameEn}</b> (${p.variant})<br>
            <small style="color:#64748b;">${p.nameTa || ''}</small>
          </div>
          <div style="text-align:right;">
            <b style="color:#ef4444; font-size:16px;">${p.stock} left</b><br>
            <small style="color:#94a3b8;">Alert at ≤ ${p.lowAlert}</small>
          </div>
        </div>
      `).join('');
    } else {
      lowContainer.innerHTML = `<div style="color:#10b981; font-weight:600; padding:15px 0;">✓ All stock levels are sufficient.</div>`;
    }

    // Recent transactions
    const bills = await api('GET', '/api/bills');
    const recent = bills.slice(0, 6);
    $('recentBillsContainer').innerHTML = recent.length ? recent.map(b => `
      <div style="display:flex; justify-content:space-between; align-items:center; padding:8px 0; border-bottom:1px solid #f1f5f9;">
        <div>
          <b>${b.billNo}</b> • ${b.customer.name}<br>
          <small style="color:#64748b;">${b.date} • ${b.payMode} • ${b.items.length} items</small>
        </div>
        <div>
          <b style="font-size:15px; color:#1b5e20;">${formatINR(b.grandTotal)}</b>
        </div>
      </div>
    `).join('') : '<div style="color:#64748b; padding:10px 0;">No sales recorded yet.</div>';

  } catch (err) {
    console.error('Dashboard load failed:', err);
  }
}

// --------------------------------------------------------------------------
// 6. Inventory & Catalog Management
// --------------------------------------------------------------------------
async function loadInventoryTable() {
  if (!IS_MASTER_MODE) return;
  try {
    PRODUCTS = await api('GET', '/api/products');
    const tbody = $('inventoryTableBody');
    tbody.innerHTML = PRODUCTS.map(p => `
      <tr>
        <td>
          <b>${p.nameEn}</b>
          <div style="font-size:11px; color:#64748b;">${p.nameTa || ''} • ${p.variant}</div>
        </td>
        <td>${p.category}</td>
        <td>${p.hsn || '-'}</td>
        <td><code>${p.barcode || '-'}</code></td>
        <td>${p.gstPct}%</td>
        <td>${formatINR(p.mrp)}</td>
        <td><b>${formatINR(p.price)}</b></td>
        <td><b style="color:${p.stock <= p.lowAlert ? '#ef4444' : '#1b5e20'}">${p.stock}</b> ${p.unit}</td>
        <td>${formatINR(p.stock * p.price)}</td>
        <td>
          <button class="chip-btn" onclick="openProductModal('${p.id}')">Edit</button>
          <button class="chip-btn" style="color:#ef4444;" onclick="deleteProduct('${p.id}')">Del</button>
        </td>
      </tr>
    `).join('');
  } catch (err) {
    console.error('Inventory load failed:', err);
  }
}

function openProductModal(id) {
  $('productModal').classList.add('active');
  if (id) {
    const p = PRODUCTS.find(x => x.id === id);
    if (!p) return;
    $('productModalTitle').textContent = 'Edit Product';
    $('mpId').value = p.id;
    $('mpNameEn').value = p.nameEn;
    $('mpNameTa').value = p.nameTa || '';
    $('mpCategory').value = p.category;
    $('mpVariant').value = p.variant;
    $('mpHsn').value = p.hsn || '';
    $('mpBarcode').value = p.barcode || '';
    $('mpGstPct').value = p.gstPct;
    $('mpPrice').value = p.price;
    $('mpMrp').value = p.mrp;
    $('mpStock').value = p.stock;
    $('mpLowAlert').value = p.lowAlert;
  } else {
    $('productModalTitle').textContent = 'Add New Product';
    $('mpId').value = '';
    $('mpNameEn').value = '';
    $('mpNameTa').value = '';
    $('mpVariant').value = '1 Litre';
    $('mpHsn').value = '1513';
    $('mpBarcode').value = '';
    $('mpGstPct').value = 5;
    $('mpPrice').value = '';
    $('mpMrp').value = '';
    $('mpStock').value = 50;
    $('mpLowAlert').value = 10;
  }
}

function closeProductModal() {
  $('productModal').classList.remove('active');
}

async function saveProductModal() {
  const p = {
    id: $('mpId').value || undefined,
    nameEn: $('mpNameEn').value.trim(),
    nameTa: $('mpNameTa').value.trim(),
    category: $('mpCategory').value,
    variant: $('mpVariant').value.trim(),
    hsn: $('mpHsn').value.trim(),
    barcode: $('mpBarcode').value.trim(),
    gstPct: Number($('mpGstPct').value) || 5,
    price: Number($('mpPrice').value),
    mrp: Number($('mpMrp').value) || Number($('mpPrice').value),
    stock: Number($('mpStock').value) || 0,
    lowAlert: Number($('mpLowAlert').value) || 5,
    unit: 'Bottle'
  };

  if (!p.nameEn || isNaN(p.price) || p.price < 0) {
    alert('Please enter a valid product name and price.');
    return;
  }

  try {
    await api('POST', '/api/products', p);
    closeProductModal();
    loadInventoryTable();
    PRODUCTS = await api('GET', '/api/products');
    renderProductsGrid();
  } catch (err) {
    alert('Failed to save product: ' + err.message);
  }
}

async function deleteProduct(id) {
  if (!confirm('Are you sure you want to delete this product?')) return;
  try {
    await api('DELETE', `/api/products/${id}`);
    loadInventoryTable();
    PRODUCTS = await api('GET', '/api/products');
    renderProductsGrid();
  } catch (err) {
    alert('Delete failed: ' + err.message);
  }
}

// Quick Stock Adjustment
function openAdjustModal() {
  const sel = $('adjProductSelect');
  sel.innerHTML = PRODUCTS.map(p => `<option value="${p.id}">${p.nameEn} (Stock: ${p.stock})</option>`).join('');
  $('adjQty').value = '';
  $('adjNote').value = '';
  $('adjustModal').classList.add('active');
}

function closeAdjustModal() {
  $('adjustModal').classList.remove('active');
}

async function submitStockAdjustment() {
  const pid = $('adjProductSelect').value;
  const qty = Number($('adjQty').value);
  const type = $('adjType').value;
  const note = $('adjNote').value.trim();

  if (!pid || isNaN(qty) || qty === 0) {
    alert('Please specify a valid non-zero adjustment quantity.');
    return;
  }

  try {
    await api('POST', '/api/inventory/adjust', {
      productId: pid,
      qty,
      type,
      note
    });
    closeAdjustModal();
    loadInventoryTable();
    loadStockMoves();
    PRODUCTS = await api('GET', '/api/products');
    renderProductsGrid();
  } catch (err) {
    alert('Adjustment failed: ' + err.message);
  }
}

// --------------------------------------------------------------------------
// 7. Production Batching & Stock Ledger
// --------------------------------------------------------------------------
async function loadProductionTable() {
  if (!IS_MASTER_MODE) return;
  try {
    PRODUCTS = await api('GET', '/api/products');
    $('productionTableBody').innerHTML = PRODUCTS.map(p => `
      <tr>
        <td>
          <b>${p.nameEn}</b> (${p.variant})
          <div style="font-size:11px; color:#64748b;">${p.nameTa || ''}</div>
        </td>
        <td><b>${p.stock}</b> ${p.unit}</td>
        <td>
          <input type="number" min="0" value="0" class="prod-batch-qty" data-pid="${p.id}" style="width:100px; padding:6px; font-weight:bold;">
        </td>
      </tr>
    `).join('');
  } catch (err) {
    console.error('Production table load failed:', err);
  }
}

async function submitProductionBatch() {
  const inputs = document.querySelectorAll('.prod-batch-qty');
  const items = [];
  inputs.forEach(inp => {
    const qty = Number(inp.value) || 0;
    if (qty > 0) {
      items.push({ productId: inp.dataset.pid, qty });
    }
  });

  if (!items.length) {
    alert('Please enter quantities for at least one finished oil product.');
    return;
  }

  try {
    await api('POST', '/api/inventory/production', {
      date: $('prodBatchDate').value || new Date().toISOString().slice(0, 10),
      note: $('prodBatchNote').value || 'Marachekku Woodpress Crushing Batch',
      items
    });
    alert('✅ Finished oil stock credited successfully.');
    loadProductionTable();
    loadStockMoves();
    PRODUCTS = await api('GET', '/api/products');
    renderProductsGrid();
  } catch (err) {
    alert('Failed to commit production batch: ' + err.message);
  }
}

async function loadStockMoves() {
  if (!IS_MASTER_MODE) return;
  try {
    const moves = await api('GET', '/api/inventory/moves?limit=100');
    const list = moves || [];
    if (!list.length) {
      $('stockMovesBody').innerHTML = `<tr><td colspan="5" class="center muted-text" style="padding:16px;">No stock movements recorded yet</td></tr>`;
      return;
    }
    $('stockMovesBody').innerHTML = list.map(m => {
      const p = PRODUCTS.find(x => x.id === m.productId);
      return `
        <tr>
          <td>${m.date}</td>
          <td><b>${p ? p.nameEn : m.productId}</b></td>
          <td><span class="chip-btn">${m.type}</span></td>
          <td style="font-weight:bold; color:${m.qty >= 0 ? '#10b981' : '#ef4444'}">
            ${m.qty > 0 ? '+' : ''}${m.qty}
          </td>
          <td>${m.note}</td>
        </tr>
      `;
    }).join('');
  } catch (err) {
    console.error('Stock moves load failed:', err);
  }
}

// --------------------------------------------------------------------------
// 8. Purchases & Expenses Management
// --------------------------------------------------------------------------
async function loadPurchases() {
  if (!IS_MASTER_MODE) return;
  try {
    const list = await api('GET', '/api/purchases') || [];
    if (!list.length) {
      $('purchasesTableBody').innerHTML = `<tr><td colspan="6" class="center muted-text" style="padding:16px;">No procurement entries recorded yet</td></tr>`;
      return;
    }
    $('purchasesTableBody').innerHTML = list.map(p => `
      <tr>
        <td>${p.date}</td>
        <td><b>${p.item}</b><br><small style="color:#64748b;">${p.note || ''}</small></td>
        <td>${p.qty} ${p.unit}</td>
        <td><b>${formatINR(p.cost)}</b></td>
        <td>${p.supplier || '-'}</td>
        <td>
          <button class="del-btn" onclick="deletePurchase('${p.id}')">✕</button>
        </td>
      </tr>
    `).join('');
  } catch (err) {
    console.error('Purchases load failed:', err);
  }
}

async function savePurchase() {
  const item = $('puItem').value.trim();
  const cost = Number($('puCost').value);
  if (!item || isNaN(cost) || cost < 0) {
    alert('Please enter a valid item name and total cost.');
    return;
  }

  try {
    await api('POST', '/api/purchases', {
      date: $('puDate').value,
      supplier: $('puSupplier').value.trim(),
      item,
      qty: Number($('puQty').value) || 0,
      unit: $('puUnit').value,
      cost,
      note: $('puNote').value.trim()
    });
    $('puItem').value = '';
    $('puCost').value = '';
    $('puQty').value = '';
    loadPurchases();
    loadDashboard();
  } catch (err) {
    alert('Failed to save purchase: ' + err.message);
  }
}

async function deletePurchase(id) {
  if (!confirm('Delete purchase entry?')) return;
  try {
    await api('DELETE', `/api/purchases/${id}`);
    loadPurchases();
    loadDashboard();
  } catch (err) {
    alert('Delete failed: ' + err.message);
  }
}

async function loadExpenses() {
  if (!IS_MASTER_MODE) return;
  try {
    const list = await api('GET', '/api/expenses') || [];
    if (!list.length) {
      $('expensesTableBody').innerHTML = `<tr><td colspan="5" class="center muted-text" style="padding:16px;">No operational overheads recorded yet</td></tr>`;
      return;
    }
    $('expensesTableBody').innerHTML = list.map(e => `
      <tr>
        <td>${e.date}</td>
        <td><b>${e.head}</b></td>
        <td><b>${formatINR(e.amount)}</b></td>
        <td>${e.note || '-'}</td>
        <td>
          <button class="del-btn" onclick="deleteExpense('${e.id}')">✕</button>
        </td>
      </tr>
    `).join('');
  } catch (err) {
    console.error('Expenses load failed:', err);
  }
}

async function saveExpense() {
  const amt = Number($('exAmt').value);
  if (isNaN(amt) || amt <= 0) {
    alert('Please enter a valid expense amount.');
    return;
  }

  try {
    await api('POST', '/api/expenses', {
      date: $('exDate').value,
      head: $('exHead').value,
      amount: amt,
      note: $('exNote').value.trim()
    });
    $('exAmt').value = '';
    $('exNote').value = '';
    loadExpenses();
    loadDashboard();
  } catch (err) {
    alert('Failed to save expense: ' + err.message);
  }
}

async function deleteExpense(id) {
  if (!confirm('Delete expense entry?')) return;
  try {
    await api('DELETE', `/api/expenses/${id}`);
    loadExpenses();
    loadDashboard();
  } catch (err) {
    alert('Delete failed: ' + err.message);
  }
}

// --------------------------------------------------------------------------
// 9. Sales Register, Invoices & Multi-Format Exports
// --------------------------------------------------------------------------
async function loadBills() {
  if (!IS_MASTER_MODE) return;
  try {
    const from = $('filterFrom').value;
    const to = $('filterTo').value;
    const q = [];
    if (from) q.push(`from=${from}`);
    if (to) q.push(`to=${to}`);

    ALL_BILLS = (await api('GET', '/api/bills' + (q.length ? '?' + q.join('&') : ''))) || [];
    const tbody = $('billsTableBody');
    if (!ALL_BILLS.length) {
      tbody.innerHTML = `<tr><td colspan="9" class="center muted-text" style="padding:24px;">No invoices found for the selected period</td></tr>`;
      return;
    }

    tbody.innerHTML = ALL_BILLS.map(b => `
      <tr>
        <td><b>${b.billNo}</b></td>
        <td>${b.date}</td>
        <td>${b.customer.name}</td>
        <td>${b.gstOn ? '<span class="chip-btn" style="color:#1b5e20;">GST</span>' : '<span class="chip-btn">Cash</span>'}</td>
        <td>${formatINR(b.subtotal)}</td>
        <td>${formatINR(b.gstAmt)}</td>
        <td><b style="font-size:14px;">${formatINR(b.grandTotal)}</b></td>
        <td>${b.payMode}</td>
        <td>
          <button class="chip-btn" style="color:#0284c7; font-weight:700;" onclick="editExistingBill('${b.id}')">✏️ Edit</button>
          <button class="chip-btn" onclick="reprintBill('${b.id}', '80mm')">🧻 80mm</button>
          <button class="chip-btn" onclick="reprintBill('${b.id}', 'a4')">📑 A4</button>
          <button class="chip-btn" onclick="downloadBillPDF('${b.id}')">📥 PDF</button>
          <button class="chip-btn" style="color:#ef4444;" onclick="cancelBill('${b.id}')">Cancel</button>
        </td>
      </tr>
    `).join('');
  } catch (err) {
    console.error('Bills load failed:', err);
  }
}

async function editExistingBill(id) {
  try {
    const bill = await api('GET', `/api/bills/${id}`);
    if (!bill || !bill.items) {
      alert('Could not load bill details for editing.');
      return;
    }

    // Check if tab for this bill is already open
    const existingTabIndex = BILL_TABS.findIndex(t => t.editingBillId === id);
    if (existingTabIndex !== -1) {
      switchBillTab(existingTabIndex);
      switchTab('pos');
      return;
    }

    // Open new tab for editing
    saveCurrentBillTabForm();
    const newBillTab = {
      id: Date.now(),
      title: `Edit: ${bill.billNo}`,
      editingBillId: bill.id,
      editingBillNo: bill.billNo,
      cart: bill.items.map(it => ({
        productId: it.productId,
        nameEn: it.nameEn,
        nameTa: it.nameTa,
        hsn: it.hsn,
        barcode: it.barcode,
        price: it.price,
        gstPct: it.gstPct,
        qty: it.qty
      })),
      customer: {
        name: bill.customer.name || '',
        phone: bill.customer.phone || '',
        gstin: bill.customer.gstin || '',
        address: bill.customer.address || '',
        state: bill.customer.state || 'Tamil Nadu'
      },
      gstOn: bill.gstOn !== false,
      discount: bill.discount || 0,
      paid: bill.paid || '',
      payMode: bill.payMode || 'Cash',
      date: bill.date || ''
    };

    BILL_TABS.push(newBillTab);
    ACTIVE_BILL_INDEX = BILL_TABS.length - 1;
    CART = BILL_TABS[ACTIVE_BILL_INDEX].cart;

    loadActiveBillTabForm();
    if (bill.date && $('billDate')) {
      $('billDate').value = bill.date;
    }
    renderBillTabs();
    renderCart();

    switchTab('pos');
  } catch (err) {
    alert('Failed to load bill for editing: ' + err.message);
  }
}

async function reprintBill(id, format) {
  try {
    const bill = await api('GET', `/api/bills/${id}`);
    if (IS_MASTER_MODE) {
      if (format === 'a4' || format === 'a5' || !SETTINGS.printerInterface || SETTINGS.printerInterface === 'browser') {
        PrintEngine.print(bill, format, SETTINGS);
      } else {
        await printBillDirect(bill, format);
      }
    } else {
      await api('POST', '/api/print-queue', {
        billId: bill.id,
        bill: bill,
        format: format || (SETTINGS && SETTINGS.defaultPrinter) || '80mm'
      });
      showToast(`✅ Bill ${bill.billNo} sent to Master Print Queue!`, 'success', 4000);
    }
  } catch (err) {
    alert('Failed to fetch bill for printing: ' + err.message);
  }
}

async function reprintLastBill(format) {
  if (!LAST_BILL) {
    if (ALL_BILLS && ALL_BILLS.length > 0) {
      LAST_BILL = ALL_BILLS[0];
    } else {
      try {
        const recent = await api('GET', '/api/bills?limit=1');
        if (recent && recent.length > 0) {
          LAST_BILL = recent[0];
        }
      } catch (e) {}
    }
  }
  if (!LAST_BILL) {
    alert('No recent bill to reprint.');
    return;
  }
  reprintBill(LAST_BILL.id, format || 'default');
}

async function downloadBillPDF(id) {
  try {
    const bill = await api('GET', `/api/bills/${id}`);
    PDFEngine.saveBillPDF(bill, SETTINGS);
  } catch (err) {
    alert('Failed to generate PDF: ' + err.message);
  }
}

async function cancelBill(id) {
  const pin = prompt('🔐 Master PIN Required\nPlease enter Master PIN to cancel/delete this bill:');
  if (pin === null) return; // User pressed Cancel

  const enteredPin = pin.trim();
  if (!enteredPin) {
    alert('❌ Master PIN cannot be empty. Deletion aborted.');
    return;
  }

  const validPin = (SETTINGS && SETTINGS.masterPin) ? SETTINGS.masterPin : DEFAULT_MASTER_PIN;
  if (enteredPin !== validPin) {
    alert('❌ Invalid Master PIN. Bill cancellation aborted.');
    return;
  }

  if (!confirm('⚠️ Are you sure you want to cancel this bill? Stock will be automatically restored to inventory.')) {
    return;
  }

  try {
    const res = await fetch(`/api/bills/${id}/cancel`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-Admin-Token': enteredPin
      },
      body: JSON.stringify({ pin: enteredPin })
    });

    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      throw new Error(data.error || `HTTP ${res.status}`);
    }

    alert('✅ Bill cancelled and stock restored to inventory.');
    loadBills();
    loadDashboard();
    PRODUCTS = await api('GET', '/api/products');
    renderProductsGrid();
  } catch (err) {
    alert('Cancel failed: ' + err.message);
  }
}

// Exports
function exportSalesExcel() {
  ExportEngine.exportSalesToExcel(ALL_BILLS);
}

function exportSalesCSV() {
  ExportEngine.downloadCSV(ALL_BILLS);
}

function exportSalesPDF() {
  PDFEngine.saveSalesSummaryPDF(ALL_BILLS, SETTINGS);
}

// --------------------------------------------------------------------------
// 10. GSTR-1 Tax Reports
// --------------------------------------------------------------------------
let GSTR1_DATA = null;

async function loadGSTR1Report() {
  if (!IS_MASTER_MODE) return;
  try {
    const from = $('filterFrom').value;
    const to = $('filterTo').value;
    const q = [];
    if (from) q.push(`from=${from}`);
    if (to) q.push(`to=${to}`);

    GSTR1_DATA = await api('GET', '/api/reports/gstr1' + (q.length ? '?' + q.join('&') : ''));
    
    // Cards
    $('gstr1Cards').innerHTML = `
      <div class="kpi-card">
        <div class="kpi-body">
          <div class="kpi-title">Total Taxable Value</div>
          <div class="kpi-value">${formatINR(GSTR1_DATA.totalTaxable)}</div>
        </div>
      </div>
      <div class="kpi-card">
        <div class="kpi-body">
          <div class="kpi-title">Total GST Collected</div>
          <div class="kpi-value">${formatINR(GSTR1_DATA.totalTax)}</div>
        </div>
      </div>
      <div class="kpi-card">
        <div class="kpi-body">
          <div class="kpi-title">Total Gross Turnover</div>
          <div class="kpi-value">${formatINR(GSTR1_DATA.grandTotal)}</div>
        </div>
      </div>
    `;

    // B2B Table
    $('gstr1B2BBody').innerHTML = (GSTR1_DATA.b2b || []).length ? GSTR1_DATA.b2b.map(r => `
      <tr>
        <td><code>${r.customerGstin}</code></td>
        <td><b>${r.customerName}</b></td>
        <td>${r.invoiceNo}</td>
        <td>${r.date}</td>
        <td>${formatINR(r.taxableValue)}</td>
        <td>${formatINR(r.cgst)}</td>
        <td>${formatINR(r.sgst)}</td>
        <td><b>${formatINR(r.invoiceValue)}</b></td>
      </tr>
    `).join('') : '<tr><td colspan="8" style="text-align:center; color:#64748b; padding:20px;">No B2B Invoices with customer GSTIN in this period.</td></tr>';

    // HSN Table
    $('gstr1HSNBody').innerHTML = (GSTR1_DATA.hsnSummary || []).map(r => `
      <tr>
        <td><b>${r.hsn}</b></td>
        <td>${r.description}</td>
        <td>${r.qty}</td>
        <td>${formatINR(r.totalValue)}</td>
        <td>${formatINR(r.taxableValue)}</td>
        <td>${formatINR(r.cgst)}</td>
        <td>${formatINR(r.sgst)}</td>
      </tr>
    `).join('');

  } catch (err) {
    console.error('GSTR1 report failed:', err);
  }
}

function exportGSTR1Excel() {
  if (!GSTR1_DATA) {
    alert('Please load GSTR-1 data first.');
    return;
  }
  ExportEngine.exportGSTR1ToExcel(GSTR1_DATA);
}

// --------------------------------------------------------------------------
// 11. Settings & Multi-Device Wi-Fi Setup
// --------------------------------------------------------------------------
function applySettingsToUI() {
  const isGstDefault = SETTINGS && SETTINGS.gstDefaultOn === true;

  // 1. Update POS Counter GST toggle state
  if ($('gstToggle')) {
    $('gstToggle').checked = isGstDefault;
    toggleGSTMode();
  }

  // 2. Synchronize all empty bill tabs with default GST mode
  if (BILL_TABS && BILL_TABS.length) {
    BILL_TABS.forEach(tab => {
      if (!tab.cart || tab.cart.length === 0) {
        tab.gstOn = isGstDefault;
      }
    });
    if (BILL_TABS[ACTIVE_BILL_INDEX] && (!BILL_TABS[ACTIVE_BILL_INDEX].cart || BILL_TABS[ACTIVE_BILL_INDEX].cart.length === 0)) {
      BILL_TABS[ACTIVE_BILL_INDEX].gstOn = isGstDefault;
    }
  }

  // 3. Update brand titles in header
  const brandTamil = document.querySelector('.brand-tamil');
  if (brandTamil && SETTINGS.brandEn) {
    brandTamil.innerHTML = `${SETTINGS.brandTa || 'சீராழி'} <span class="brand-en">${SETTINGS.brandEn}</span>`;
  }
  const brandSub = document.querySelector('.brand-sub');
  if (brandSub && SETTINGS.companyName) {
    brandSub.textContent = `${SETTINGS.companyName} • ${SETTINGS.tagline || 'Billing, Inventory & Multi-Print Suite'}`;
  }
}

function populateSettingsForm() {
  const s = SETTINGS || {};
  $('setCompany').value = s.companyName || '';
  $('setBrandEn').value = s.brandEn || '';
  $('setBrandTa').value = s.brandTa || '';
  $('setTagline').value = s.tagline || '';
  $('setAddr').value = s.address || '';
  $('setPhone').value = s.phone || '';
  $('setFssai').value = s.fssai || '';
  $('setGstin').value = s.gstin || '';
  $('setPrefix').value = s.billPrefix || 'VFPC';
  $('setFY').value = s.financialYear || '2026-27';
  $('setFooterEn').value = s.footerNoteEn || '';
  $('setFooterTa').value = s.footerNoteTa || '';
  $('setDefaultPrinter').value = s.defaultPrinter || '80mm';
  $('setDefaultGSTMode').value = (s.gstDefaultOn === true) ? 'true' : 'false';
  if ($('setMasterPin')) $('setMasterPin').value = s.masterPin || '1234';

  // Hardware Printer Configuration (Direct Silent Hardware Printing)
  if ($('setPrinterInterface')) $('setPrinterInterface').value = s.printerInterface || 'browser';
  if ($('setPrinterTarget')) $('setPrinterTarget').value = s.printerTarget || '';
  if ($('setPrinterIP')) $('setPrinterIP').value = (s.printerInterface === 'network' ? s.printerTarget : '') || '';
  if ($('setPrinterPort')) $('setPrinterPort').value = s.printerPort || 9100;
  if ($('setThermalPaperSize')) $('setThermalPaperSize').value = s.thermalPaperSize || s.thermalSize || '80mm';
  if ($('setPrintCopies')) $('setPrintCopies').value = s.printCopies || 1;
  if ($('setAutoCut')) $('setAutoCut').checked = (s.autoCut !== false);
  if ($('setCashDrawer')) $('setCashDrawer').checked = (s.cashDrawer === true);
  if ($('setAutoPrintMobile')) $('setAutoPrintMobile').checked = (s.autoPrintMobile !== false);

  // Secondary Multi-Printer Configuration (Dispatch / Packaging)
  if ($('setSecPrinterEnabled')) $('setSecPrinterEnabled').checked = (s.secPrinterEnabled === true);
  if ($('setSecPrinterInterface')) $('setSecPrinterInterface').value = s.secPrinterInterface || 'spooler';
  if ($('setSecPrinterTarget')) $('setSecPrinterTarget').value = s.secPrinterTarget || '';
  if ($('setSecPrinterIP')) $('setSecPrinterIP').value = (s.secPrinterInterface === 'network' ? s.secPrinterTarget : '') || '';

  togglePrinterInterfaceUI();
  toggleSecPrinterUI();
}

function generateRandomPin() {
  const pin = Math.floor(1000 + Math.random() * 9000).toString();
  if ($('setMasterPin')) {
    $('setMasterPin').value = pin;
    showToast(`🎲 Generated new PIN: ${pin}. Click Save to apply.`, 'info', 4000);
  }
}

async function saveSettingsForm() {
  const masterPin = ($('setMasterPin') ? $('setMasterPin').value.trim() : '') || (SETTINGS.masterPin || '1234');
  const printerInterface = $('setPrinterInterface') ? $('setPrinterInterface').value : (SETTINGS.printerInterface || 'browser');
  let printerTarget = '';
  if (printerInterface === 'spooler') {
    printerTarget = $('setPrinterTarget') ? $('setPrinterTarget').value.trim() : '';
  } else if (printerInterface === 'network') {
    printerTarget = $('setPrinterIP') ? $('setPrinterIP').value.trim() : '';
  }

  const secInterface = $('setSecPrinterInterface') ? $('setSecPrinterInterface').value : 'spooler';
  let secTarget = '';
  if (secInterface === 'spooler') {
    secTarget = $('setSecPrinterTarget') ? $('setSecPrinterTarget').value.trim() : '';
  } else if (secInterface === 'network') {
    secTarget = $('setSecPrinterIP') ? $('setSecPrinterIP').value.trim() : '';
  }

  const payload = {
    ...SETTINGS,
    companyName: $('setCompany').value.trim(),
    brandEn: $('setBrandEn').value.trim(),
    brandTa: $('setBrandTa').value.trim(),
    tagline: $('setTagline').value.trim(),
    address: $('setAddr').value.trim(),
    phone: $('setPhone').value.trim(),
    fssai: $('setFssai').value.trim(),
    gstin: $('setGstin').value.trim(),
    billPrefix: $('setPrefix').value.trim(),
    financialYear: $('setFY').value.trim(),
    footerNoteEn: $('setFooterEn').value.trim(),
    footerNoteTa: $('setFooterTa').value.trim(),
    defaultPrinter: $('setDefaultPrinter').value,
    gstDefaultOn: $('setDefaultGSTMode').value === 'true',
    masterPin: masterPin,

    // Hardware Printer Config
    printerInterface: printerInterface,
    printerTarget: printerTarget,
    printerPort: parseInt($('setPrinterPort') ? $('setPrinterPort').value : '9100', 10) || 9100,
    thermalPaperSize: $('setThermalPaperSize') ? $('setThermalPaperSize').value : '80mm',
    thermalSize: $('setThermalPaperSize') ? $('setThermalPaperSize').value : '80mm',
    printCopies: parseInt($('setPrintCopies') ? $('setPrintCopies').value : '1', 10) || 1,
    autoCut: $('setAutoCut') ? $('setAutoCut').checked : true,
    cashDrawer: $('setCashDrawer') ? $('setCashDrawer').checked : false,
    autoPrintMobile: $('setAutoPrintMobile') ? $('setAutoPrintMobile').checked : true,

    // Secondary Printer Config
    secPrinterEnabled: $('setSecPrinterEnabled') ? $('setSecPrinterEnabled').checked : false,
    secPrinterInterface: secInterface,
    secPrinterTarget: secTarget,
    secPrinterPort: 9100
  };

  try {
    SETTINGS = await api('PUT', '/api/settings', payload);
    if (IS_MASTER_MODE && SETTINGS.masterPin) {
      localStorage.setItem('siraazhi_admin_token', SETTINGS.masterPin);
    }
    applySettingsToUI();
    recalcCart();
    populateSettingsForm();
    alert('✅ Settings saved & synchronized with Billing Counter.');
  } catch (err) {
    alert('Failed to save settings: ' + err.message);
  }
}

// --------------------------------------------------------------------------
// Hardware Multi-Printer UI Handlers & Direct Print Dispatcher
// --------------------------------------------------------------------------

async function loadInstalledPrintersList() {
  try {
    const res = await api('GET', '/api/printers');
    const printers = (res && res.printers) || [];
    const select = $('setPrinterTarget');
    const secSelect = $('setSecPrinterTarget');

    const currentPrimary = (SETTINGS && SETTINGS.printerTarget) || (select ? select.value : '');
    const currentSec = (SETTINGS && SETTINGS.secPrinterTarget) || (secSelect ? secSelect.value : '');

    if (select) {
      if (printers.length === 0) {
        select.innerHTML = '<option value="">-- No Windows Printers Detected (Use Network IP or Browser) --</option>';
      } else {
        select.innerHTML = '<option value="">-- Select Installed Windows Printer --</option>' +
          printers.map(p => `<option value="${escapeHtml(p)}" ${p === currentPrimary ? 'selected' : ''}>${escapeHtml(p)}</option>`).join('');
      }
    }

    if (secSelect) {
      if (printers.length === 0) {
        secSelect.innerHTML = '<option value="">-- No Windows Printers Detected --</option>';
      } else {
        secSelect.innerHTML = '<option value="">-- Select Secondary Printer --</option>' +
          printers.map(p => `<option value="${escapeHtml(p)}" ${p === currentSec ? 'selected' : ''}>${escapeHtml(p)}</option>`).join('');
      }
    }
  } catch (err) {
    console.warn('Could not enumerate printers:', err);
  }
}

function togglePrinterInterfaceUI() {
  const iface = $('setPrinterInterface') ? $('setPrinterInterface').value : 'browser';
  const spoolerBox = $('groupSpoolerPrinter');
  const netBox = $('groupNetworkPrinter');

  if (spoolerBox) spoolerBox.style.display = (iface === 'spooler') ? 'block' : 'none';
  if (netBox) netBox.style.display = (iface === 'network') ? 'flex' : 'none';

  if (iface === 'spooler') {
    loadInstalledPrintersList();
  }
}

function toggleSecPrinterUI() {
  const enabled = $('setSecPrinterEnabled') && $('setSecPrinterEnabled').checked;
  const group = $('groupSecPrinter');
  if (group) group.style.display = enabled ? 'block' : 'none';
  if (enabled) {
    toggleSecPrinterTargetUI();
  }
}

function toggleSecPrinterTargetUI() {
  const iface = $('setSecPrinterInterface') ? $('setSecPrinterInterface').value : 'spooler';
  const spoolerBox = $('secPrinterSpoolerBox');
  const netBox = $('secPrinterNetworkBox');
  if (spoolerBox) spoolerBox.style.display = (iface === 'spooler') ? 'block' : 'none';
  if (netBox) netBox.style.display = (iface === 'network') ? 'block' : 'none';
}

async function testHardwarePrinterSlip() {
  const iface = $('setPrinterInterface') ? $('setPrinterInterface').value : 'browser';
  if (iface === 'browser') {
    alert('Please select "Windows USB / Driver Spooler" or "Network LAN / Wi-Fi Printer" to test direct hardware printing.');
    return;
  }

  let target = '';
  if (iface === 'spooler') {
    target = $('setPrinterTarget') ? $('setPrinterTarget').value.trim() : '';
    if (!target) {
      alert('Please select a target Windows Printer first.');
      return;
    }
  } else if (iface === 'network') {
    target = $('setPrinterIP') ? $('setPrinterIP').value.trim() : '';
    if (!target) {
      alert('Please enter the Printer IP Address first.');
      return;
    }
  }

  const port = parseInt($('setPrinterPort') ? $('setPrinterPort').value : '9100', 10) || 9100;
  const paperSize = $('setThermalPaperSize') ? $('setThermalPaperSize').value : '80mm';

  try {
    showToast('Sending test receipt to printer...', 'info', 3000);
    const res = await api('POST', '/api/printers/test', {
      target: target,
      interface: iface,
      port: port,
      paperSize: paperSize
    });
    alert(`✅ Test Print Successful!\nTarget: ${target} (${iface})\nHardware receipt has been dispatched.`);
  } catch (err) {
    alert(`❌ Test Print Failed: ${err.message}\n\nPlease check printer USB connection, driver name, or network IP.`);
  }
}

async function printBillDirect(bill, format) {
  if (!SETTINGS.printerInterface || SETTINGS.printerInterface === 'browser') {
    PrintEngine.print(bill, format, SETTINGS);
    return;
  }

  try {
    const res = await api('POST', '/api/print-direct', {
      bill: bill,
      billId: bill.id,
      format: format || SETTINGS.thermalPaperSize || '80mm'
    });
    showToast(`🖨 Bill ${bill.billNo} sent to hardware printer.`, 'success', 3500);
  } catch (err) {
    console.warn('Hardware direct print failed, falling back to browser print dialog:', err);
    showToast(`⚠️ Hardware print error: ${err.message}. Opening print preview...`, 'warning', 4000);
    PrintEngine.print(bill, format, SETTINGS);
  }
}

// Network / LAN Mobile Modal
async function showNetworkModal() {
  try {
    const info = await api('GET', '/api/network-info');
    const container = $('networkUrlsContainer');
    
    container.innerHTML = `
      <div style="font-size:14px; margin-bottom:10px; color:#1b5e20;">Open in any Mobile or Tablet on your Wi-Fi:</div>
      ${info.urls.map(u => `
        <div style="margin:6px 0;">
          <a href="${u}" target="_blank" style="color:#0284c7; text-decoration:underline;">${u}</a>
        </div>
      `).join('')}
    `;

    // Render simple QR Code image pointing to the mobile billing URL
    const targetUrl = info.urls[0] || `http://${window.location.host}`;
    $('qrcodeDisplay').innerHTML = `
      <img src="https://api.qrserver.com/v1/create-qr-code/?size=180x180&data=${encodeURIComponent(targetUrl)}" 
           alt="Scan QR for Mobile Billing" 
           style="border:1px solid #ddd; border-radius:6px;">
      <div style="font-size:11px; color:#64748b; margin-top:6px;">Scan to open on phone camera</div>
    `;

    $('networkModal').classList.add('active');
  } catch (err) {
    alert('Network info failed: ' + err.message);
  }
}

function closeNetworkModal() {
  $('networkModal').classList.remove('active');
}

function closeAllModals() {
  closeProductModal();
  closeAdjustModal();
  closeNetworkModal();
  closePrintQueueModal();
}

// --------------------------------------------------------------------------
// Toast Notification Engine
// --------------------------------------------------------------------------
function showToast(message, type = 'success', duration = 3500) {
  const container = $('toastNotificationContainer');
  if (!container) return;

  const toast = document.createElement('div');
  toast.className = `toast-message ${type}`;
  toast.innerHTML = `<span>${type === 'success' ? '✅' : '🔔'}</span> <span>${escapeHtml(message)}</span>`;
  container.appendChild(toast);

  setTimeout(() => {
    toast.style.transition = 'opacity 0.4s ease, transform 0.4s ease';
    toast.style.opacity = '0';
    toast.style.transform = 'translateY(10px)';
    setTimeout(() => toast.remove(), 400);
  }, duration);
}

// --------------------------------------------------------------------------
// Mobile POS View Switcher & Floating Cart Bar
// --------------------------------------------------------------------------
let CURRENT_MOBILE_POS_VIEW = 'catalog'; // 'catalog' or 'cart'

function switchMobilePosView(view) {
  CURRENT_MOBILE_POS_VIEW = view;
  const catPanel = $('posCatalogPanel');
  const cartPanel = $('posCartPanel');
  const btnCatalog = $('btnShowCatalog');
  const btnCart = $('btnShowCart');
  const floatingBar = $('mobileFloatingCartBar');

  if (window.innerWidth <= 850) {
    if (view === 'cart') {
      if (catPanel) catPanel.classList.add('mobile-hidden');
      if (cartPanel) cartPanel.classList.remove('mobile-hidden');
      if (btnCatalog) btnCatalog.classList.remove('active');
      if (btnCart) btnCart.classList.add('active');
      if (floatingBar) floatingBar.style.display = 'none';
      window.scrollTo({ top: 0, behavior: 'smooth' });
    } else {
      if (catPanel) catPanel.classList.remove('mobile-hidden');
      if (cartPanel) cartPanel.classList.add('mobile-hidden');
      if (btnCatalog) btnCatalog.classList.add('active');
      if (btnCart) btnCart.classList.remove('active');
      updateMobileFloatingCart();
      window.scrollTo({ top: 0, behavior: 'smooth' });
    }
  } else {
    // Desktop / Tablet landscape shows both side-by-side
    if (catPanel) catPanel.classList.remove('mobile-hidden');
    if (cartPanel) cartPanel.classList.remove('mobile-hidden');
    if (floatingBar) floatingBar.style.display = 'none';
  }
}

function updateMobileFloatingCart() {
  const floatingBar = $('mobileFloatingCartBar');
  const badge = $('mobileCartItemCount');
  const cartBadge = $('mobileCartBadge');
  if (!floatingBar) return;

  const count = CART.reduce((sum, it) => sum + (it.qty || 1), 0);
  const total = CART.reduce((sum, it) => sum + (it.qty * it.price), 0);

  if (badge) badge.textContent = count;
  if (cartBadge) cartBadge.textContent = count;

  if (window.innerWidth <= 850 && CURRENT_MOBILE_POS_VIEW === 'catalog' && count > 0) {
    $('mfcbCount').textContent = `${count} item${count > 1 ? 's' : ''} in bill`;
    $('mfcbTotal').textContent = formatINR(total);
    floatingBar.style.display = 'flex';
  } else {
    floatingBar.style.display = 'none';
  }
}

window.addEventListener('resize', () => {
  switchMobilePosView(CURRENT_MOBILE_POS_VIEW);
});

// --------------------------------------------------------------------------
// Master System Print Queue Spooler & Management
// --------------------------------------------------------------------------
let printQueuePollTimer = null;

function startMasterPrintQueuePoller() {
  if (printQueuePollTimer) return;
  checkMasterPrintQueue();
  printQueuePollTimer = setInterval(checkMasterPrintQueue, 2500);
}

async function checkMasterPrintQueue() {
  if (!IS_MASTER_MODE) return;
  try {
    const res = await api('GET', '/api/print-queue?status=pending');
    const badge = $('printQueueBadge');
    const count = res.pendingCount || 0;
    if (badge) {
      badge.textContent = count;
      badge.style.display = count > 0 ? 'inline-block' : 'none';
    }

    const autoPrint = localStorage.getItem('siraazhi_autoprint') !== 'false';
    if (autoPrint && res.jobs && res.jobs.length > 0) {
      for (const job of res.jobs) {
        if (job.status === 'pending' && job.bill) {
          // Trigger print on Master System's physical hardware printer or browser preview
          if (job.format === 'a4' || job.format === 'a5' || !SETTINGS.printerInterface || SETTINGS.printerInterface === 'browser') {
            PrintEngine.print(job.bill, job.format, SETTINGS);
          } else {
            await printBillDirect(job.bill, job.format);
          }
          // Mark completed in queue
          await api('POST', `/api/print-queue/${job.id}/complete`);
          showToast(`🔔 Remote Bill #${job.billNo} received & sent to printer!`, 'info', 4000);
          loadDashboard();
          loadBills();
        }
      }
      checkMasterPrintQueue();
    }
  } catch (err) {
    // Silent ignore for minor polling hiccups
  }
}

async function showPrintQueueModal() {
  $('printQueueModal').classList.add('active');
  const autoPrintToggle = $('autoPrintToggle');
  if (autoPrintToggle) {
    autoPrintToggle.checked = localStorage.getItem('siraazhi_autoprint') !== 'false';
  }
  loadPrintQueueModalData();
}

function closePrintQueueModal() {
  $('printQueueModal').classList.remove('active');
}

function toggleAutoPrintRemote(checked) {
  localStorage.setItem('siraazhi_autoprint', checked ? 'true' : 'false');
  showToast(checked ? '⚡ Auto-print enabled for incoming mobile bills' : '⏸ Auto-print paused. Bills will wait in queue.');
}

async function loadPrintQueueModalData() {
  const tbody = $('printQueueTableBody');
  if (!tbody) return;
  try {
    const res = await api('GET', '/api/print-queue');
    const jobs = res.jobs || [];
    if (!jobs.length) {
      tbody.innerHTML = `<tr><td colspan="7" class="center muted-text" style="padding:24px;">No print jobs in queue</td></tr>`;
      return;
    }

    tbody.innerHTML = jobs.map(j => {
      const isPending = j.status === 'pending';
      const timeStr = new Date(j.createdAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
      const cust = j.bill && j.bill.customer ? j.bill.customer.name : '-';
      const total = j.bill ? formatINR(j.bill.grandTotal) : '-';

      return `
        <tr>
          <td><b>${escapeHtml(j.billNo)}</b></td>
          <td style="font-size:12px;">${timeStr}</td>
          <td><span style="font-size:11px; font-family:monospace; background:#e2e8f0; padding:2px 6px; border-radius:4px;">${escapeHtml(j.clientIp || 'LAN')}</span></td>
          <td>${escapeHtml(cust)}</td>
          <td style="font-weight:700;">${total}</td>
          <td><span class="status-badge ${j.status}">${j.status}</span></td>
          <td class="center">
            ${isPending ? 
              `<button class="btn btn-sm btn-primary" onclick="executeManualPrintJob('${j.id}')">🖨️ Print Now</button>` : 
              `<button class="btn btn-sm btn-secondary" onclick="executeManualPrintJob('${j.id}')">Reprint</button>`
            }
            <button class="del-btn" onclick="deletePrintJob('${j.id}')" title="Delete job">✕</button>
          </td>
        </tr>
      `;
    }).join('');
  } catch (err) {
    tbody.innerHTML = `<tr><td colspan="7" class="center" style="color:red; padding:16px;">Failed to load queue: ${err.message}</td></tr>`;
  }
}

async function executeManualPrintJob(jobId) {
  try {
    const res = await api('GET', '/api/print-queue');
    const job = (res.jobs || []).find(j => j.id === jobId);
    if (!job || !job.bill) {
      alert('Job or bill data not found');
      return;
    }
    if (job.format === 'a4' || job.format === 'a5' || !SETTINGS.printerInterface || SETTINGS.printerInterface === 'browser') {
      PrintEngine.print(job.bill, job.format, SETTINGS);
    } else {
      await printBillDirect(job.bill, job.format);
    }
    await api('POST', `/api/print-queue/${job.id}/complete`);
    loadPrintQueueModalData();
    checkMasterPrintQueue();
  } catch (err) {
    alert('Print error: ' + err.message);
  }
}

async function deletePrintJob(jobId) {
  try {
    await api('DELETE', `/api/print-queue/${jobId}`);
    loadPrintQueueModalData();
    checkMasterPrintQueue();
  } catch (err) {
    alert('Delete error: ' + err.message);
  }
}

async function clearFinishedPrintJobs() {
  try {
    await api('DELETE', '/api/print-queue');
    loadPrintQueueModalData();
    checkMasterPrintQueue();
    showToast('🧹 Cleared printed jobs from history');
  } catch (err) {
    alert('Clear error: ' + err.message);
  }
}

function downloadBackupJSON() {
  window.open('/api/backup', '_blank');
}

async function restoreBackupFile(input) {
  const file = input.files[0];
  if (!file) return;

  try {
    const text = await file.text();
    const json = JSON.parse(text);
    await api('POST', '/api/restore', json);
    alert('✅ Database restored successfully. Reloading...');
    location.reload();
  } catch (err) {
    alert('Restore failed: ' + err.message);
  }
}

// Bootstrap on DOM Load
window.addEventListener('DOMContentLoaded', initApp);
