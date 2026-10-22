const { chromium } = require('playwright-core');
const path = require('path');
const fs = require('fs');

const EDGE_PATH = 'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe';
const OUT_DIR = path.join(__dirname, 'screenshots');

async function capture() {
  if (!fs.existsSync(OUT_DIR)) {
    fs.mkdirSync(OUT_DIR, { recursive: true });
  }

  const browser = await chromium.launch({
    executablePath: EDGE_PATH,
    headless: true,
    args: ['--no-sandbox', '--disable-gpu']
  });

  const context = await browser.newContext({
    viewport: { width: 1366, height: 768 },
    deviceScaleFactor: 2
  });

  const page = await context.newPage();
  page.on('dialog', dialog => dialog.accept());

  console.log('Navigating to POS on http://localhost:8085...');
  await page.goto('http://localhost:8085', { waitUntil: 'networkidle' });
  await page.waitForTimeout(1000);

  // 1. Dashboard Tab
  console.log('Capturing 01_dashboard.png...');
  await page.evaluate(() => switchTab('dash'));
  await page.waitForTimeout(600);
  await page.screenshot({ path: path.join(OUT_DIR, '01_dashboard.png') });

  // 2. POS Billing Tab (Clean Catalog)
  console.log('Capturing 02_pos_billing.png...');
  await page.evaluate(() => {
    switchTab('pos');
    if (typeof clearCart === 'function') clearCart();
  });
  await page.waitForTimeout(600);
  await page.screenshot({ path: path.join(OUT_DIR, '02_pos_billing.png') });

  // 3. Active Cart with items, customer details, and Quick Cash calculation
  console.log('Capturing 03_active_cart.png...');
  await page.evaluate(() => {
    switchTab('pos');
    if (typeof addToCart === 'function' && PRODUCTS && PRODUCTS.length >= 2) {
      addToCart(PRODUCTS[0].id);
      addToCart(PRODUCTS[0].id);
      addToCart(PRODUCTS[1].id);
    }
    const nameInput = document.getElementById('custName');
    const phoneInput = document.getElementById('custPhone');
    if (nameInput) nameInput.value = 'Kavitha R.';
    if (phoneInput) phoneInput.value = '9876543210';
    if (typeof setQuickCash === 'function') {
      setQuickCash(1000);
    }
  });
  await page.waitForTimeout(600);
  await page.screenshot({ path: path.join(OUT_DIR, '03_active_cart.png') });

  // Clear cart before moving to inventory
  await page.evaluate(() => { if (typeof clearCart === 'function') clearCart(); });
  await page.waitForTimeout(200);

  // 4. Inventory Tab
  console.log('Capturing 04_inventory.png...');
  await page.evaluate(() => switchTab('inventory'));
  await page.waitForTimeout(700);
  await page.screenshot({ path: path.join(OUT_DIR, '04_inventory.png') });

  // 5. Production Tab
  console.log('Capturing 05_production.png...');
  await page.evaluate(() => switchTab('production'));
  await page.waitForTimeout(700);
  await page.screenshot({ path: path.join(OUT_DIR, '05_production.png') });

  // 6. Purchases Tab
  console.log('Capturing 06_purchases.png...');
  await page.evaluate(() => switchTab('purchases'));
  await page.waitForTimeout(700);
  await page.screenshot({ path: path.join(OUT_DIR, '06_purchases.png') });

  // 7. Expenses Tab
  console.log('Capturing 07_expenses.png...');
  await page.evaluate(() => switchTab('expenses'));
  await page.waitForTimeout(700);
  await page.screenshot({ path: path.join(OUT_DIR, '07_expenses.png') });

  // 8. Sales History Tab
  console.log('Capturing 08_sales_history.png...');
  await page.evaluate(() => switchTab('bills'));
  await page.waitForTimeout(700);
  await page.screenshot({ path: path.join(OUT_DIR, '08_sales_history.png') });

  // 9. GSTR-1 Tax Report Tab
  console.log('Capturing 09_reports_gstr1.png...');
  await page.evaluate(() => switchTab('gstr1'));
  await page.waitForTimeout(700);
  await page.screenshot({ path: path.join(OUT_DIR, '09_reports_gstr1.png') });

  // 10. Settings Tab
  console.log('Capturing 10_settings.png...');
  await page.evaluate(() => switchTab('settings'));
  await page.waitForTimeout(700);
  await page.screenshot({ path: path.join(OUT_DIR, '10_settings.png') });

  // 11. Mobile Access / Wi-Fi QR Code Modal
  console.log('Capturing 11_mobile_wifi_modal.png...');
  await page.evaluate(() => {
    switchTab('pos');
    if (typeof showNetworkModal === 'function') showNetworkModal();
  });
  await page.waitForTimeout(700);
  await page.screenshot({ path: path.join(OUT_DIR, '11_mobile_wifi_modal.png') });

  // 12. Hardware Print Queue Modal
  console.log('Capturing 12_print_queue.png...');
  await page.evaluate(() => {
    if (typeof closeNetworkModal === 'function') closeNetworkModal();
    if (typeof openPrintQueueModal === 'function') openPrintQueueModal();
  });
  await page.waitForTimeout(700);
  await page.screenshot({ path: path.join(OUT_DIR, '12_print_queue.png') });

  await browser.close();
  console.log('All 12 user manual screenshots captured successfully in docs/screenshots!');
}

capture().catch(err => {
  console.error('Capture failed:', err);
  process.exit(1);
});
