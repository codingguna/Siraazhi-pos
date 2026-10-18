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
let CART = [];
let ALL_BILLS = [];
let ACTIVE_CATEGORY = 'all';
let IS_MASTER_MODE = true; // Determined dynamically below
const DEFAULT_MASTER_PIN = '1234';

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

  determineRoleMode();
  setupKeyboardShortcuts();

  try {
    SETTINGS = await api('GET', '/api/settings');
    PRODUCTS = await api('GET', '/api/products');
    
    // Apply default GST toggle from settings
    $('gstToggle').checked = SETTINGS.gstDefaultOn !== false;
    toggleGSTMode();

    renderProductsGrid();
    renderCart();

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

  // Master mode if running locally on host PC or valid unlock token present
  if (isLocalHost || savedToken === DEFAULT_MASTER_PIN) {
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

  if ($('unlockMasterBtn')) {
    $('unlockMasterBtn').remove();
  }
}

/**
 * promptMasterPin prompts the user for admin PIN to upgrade to master mode.
 */
function promptMasterPin() {
  const pin = prompt('Enter Master Admin PIN to unlock full management & reports:');
  if (pin === DEFAULT_MASTER_PIN) {
    localStorage.setItem('siraazhi_admin_token', pin);
    alert('✅ Master Access Granted.');
    location.reload();
  } else if (pin !== null) {
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
  CART = [];
  $('billDiscount').value = 0;
  $('custName').value = 'Walk-in';
  $('custPhone').value = '';
  $('custGstin').value = '';
  $('custAddress').value = '';
  $('billPaid').value = '';
  renderCart();
}

/**
 * toggleGSTMode switches between Tax Invoice (with GST breakdown) and Cash Bill.
 */
function toggleGSTMode() {
  const isGst = $('gstToggle').checked;
  $('gstToggleLabel').innerHTML = isGst ? 
    '<b>GST Invoice</b> (Tax split)' : 
    '<b>Cash Bill</b> (No tax)';
  $('gstDetailsRow').style.display = isGst ? 'flex' : 'none';
  $('taxBreakdownRow').style.display = isGst ? 'flex' : 'none';
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
 * Flow: compiles payload -> calls POST /api/bills -> receives saved invoice -> routes to PrintEngine or PDFEngine.
 */
async function checkoutBill(printFormat) {
  if (!CART.length) {
    alert('Please add at least one item to the cart.');
    return;
  }

  const isGst = $('gstToggle').checked;
  const grandTotal = Math.max(0, CART.reduce((s, it) => s + (it.qty * it.price), 0) - Number($('billDiscount').value || 0));
  const paidVal = $('billPaid').value ? Number($('billPaid').value) : grandTotal;

  const payload = {
    date: $('billDate').value || new Date().toISOString().slice(0, 10),
    customer: {
      name: $('custName').value || 'Walk-in',
      phone: $('custPhone').value || '',
      address: $('custAddress').value || '',
      gstin: $('custGstin').value || '',
      state: 'Tamil Nadu'
    },
    items: CART,
    discount: Number($('billDiscount').value) || 0,
    gstOn: isGst,
    payMode: $('billPayMode').value || 'Cash',
    paid: paidVal,
    note: ''
  };

  try {
    const bill = await api('POST', '/api/bills', payload);
    
    // Refresh product stock cache
    PRODUCTS = await api('GET', '/api/products');
    renderProductsGrid();

    // Print or PDF Export
    if (printFormat === 'pdf') {
      PDFEngine.saveBillPDF(bill, SETTINGS);
    } else {
      PrintEngine.print(bill, printFormat, SETTINGS);
    }

    clearCart();

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
    } else if (e.key === 'F10') {
      e.preventDefault();
      checkoutBill('default');
    } else if (e.key === 'Escape') {
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
    $('stockMovesBody').innerHTML = moves.map(m => {
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
    const list = await api('GET', '/api/purchases');
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
    const list = await api('GET', '/api/expenses');
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

    ALL_BILLS = await api('GET', '/api/bills' + (q.length ? '?' + q.join('&') : ''));
    const tbody = $('billsTableBody');

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

async function reprintBill(id, format) {
  try {
    const bill = await api('GET', `/api/bills/${id}`);
    PrintEngine.print(bill, format, SETTINGS);
  } catch (err) {
    alert('Failed to fetch bill for printing: ' + err.message);
  }
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
  if (!confirm('⚠️ Are you sure you want to cancel this bill? Stock will be automatically restored.')) return;
  try {
    await api('POST', `/api/bills/${id}/cancel`);
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
function populateSettingsForm() {
  const s = SETTINGS;
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
  $('setDefaultGSTMode').value = s.gstDefaultOn !== false ? 'true' : 'false';
}

async function saveSettingsForm() {
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
    gstDefaultOn: $('setDefaultGSTMode').value === 'true'
  };

  try {
    SETTINGS = await api('PUT', '/api/settings', payload);
    alert('✅ Settings saved successfully.');
  } catch (err) {
    alert('Failed to save settings: ' + err.message);
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
