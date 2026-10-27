/* ==========================================================================
   Siraazhi Pro POS — Vector PDF Export Engine
   
   WHAT IT IS:
     Client-side PDF generation engine that creates pristine vector-sharp
     tax invoices, cash receipts, and sales ledger statements for digital sharing.

   HOW IT WORKS:
     1. saveBillPDF(bill, settings):
        - Spawns a clean print preview window with document title set to:
          `Invoice_<BillNo>_<CustomerName>` (which becomes the default PDF file name).
        - Injects corporate A4 tax invoice markup with FSSAI, GSTIN, HSN table, and sign-offs.
        - Automatically triggers `window.print()` targeting 'Save as PDF'.
        - Includes an in-page fallback if pop-ups are blocked by the browser.
     2. saveSalesSummaryPDF(bills, settings):
        - Formats the complete sales register into an A4 landscape executive report.
        - Summarizes net turnover, total GST collected, and individual bill statuses.
        - Triggers instant PDF printing with in-page fallback.

   FLOW:
     Bill Object -> HTML Document Template -> Styled Browser Window -> Trigger Print / Save PDF
   ========================================================================== */

const PDFEngine = {
  /**
   * Escape HTML utility to prevent syntax breakages
   */
  esc(str) {
    return String(str || '').replace(/[&<>'"]/g, tag => ({
      '&': '&amp;',
      '<': '&lt;',
      '>': '&gt;',
      "'": '&#39;',
      '"': '&quot;'
    }[tag] || tag));
  },

  /**
   * getA4Styles returns complete self-contained styles for vector A4 corporate invoices.
   * Eliminates dependencies on external network stylesheets so white-page issues never occur.
   */
  getA4Styles() {
    return `
      @page {
        size: A4 portrait;
        margin: 8mm 10mm;
      }
      * {
        box-sizing: border-box;
      }
      body {
        margin: 0;
        padding: 0;
        background: #fff;
        color: #000;
        font-family: 'Segoe UI', Nirmala UI, Arial, sans-serif;
        -webkit-print-color-adjust: exact;
        print-color-adjust: exact;
      }
      .print-container,
      .invoice-pdf-container {
        display: block !important;
        visibility: visible !important;
        width: 100%;
        margin: 0 auto;
        padding: 0;
      }
      .invoice-a4 {
        display: block !important;
        visibility: visible !important;
        width: 100%;
        max-width: 190mm;
        margin: 0 auto;
        padding: 8mm 10mm;
        font-family: 'Segoe UI', Nirmala UI, Arial, sans-serif;
        font-size: 12px;
        color: #000;
        background: #fff;
      }
      .invoice-a4-two-up {
        width: 100%;
        max-width: 198mm;
        height: 285mm;
        margin: 0 auto;
        box-sizing: border-box;
        display: flex;
        flex-direction: column;
        justify-content: space-between;
        page-break-inside: avoid;
        page-break-after: avoid;
      }
      .invoice-a4.invoice-a4-half {
        padding: 3mm 8mm;
        font-size: 9.5px;
        line-height: 1.20;
        height: 137mm;
        max-height: 137mm;
        box-sizing: border-box;
        display: flex;
        flex-direction: column;
        justify-content: space-between;
        page-break-inside: avoid;
        page-break-after: avoid;
        overflow: hidden;
      }
      .invoice-a4.invoice-a4-half .half-top-block {
        display: flex;
        flex-direction: column;
      }
      .invoice-a4.invoice-a4-half .half-bottom-block {
        display: flex;
        flex-direction: column;
      }
      .invoice-a4.invoice-a4-half .header-block {
        margin-bottom: 3px;
        padding-bottom: 2px;
      }
      .invoice-a4.invoice-a4-half h1 {
        font-size: 13px;
        margin-bottom: 1px;
      }
      .invoice-a4.invoice-a4-half .badge-invoice {
        font-size: 10px;
        padding: 2px 6px;
      }
      .invoice-a4.invoice-a4-half .meta-grid {
        padding: 3px 6px;
        margin-bottom: 4px;
        font-size: 9px;
        gap: 4px;
      }
      .invoice-a4.invoice-a4-half table.items-table {
        margin-bottom: 3px;
      }
      .invoice-a4.invoice-a4-half table.items-table th,
      .invoice-a4.invoice-a4-half table.items-table td {
        padding: 2px 4px;
        font-size: 9px;
      }
      .invoice-a4.invoice-a4-half table.items-table tr.blank-row td {
        height: 18px;
      }
      .invoice-a4.invoice-a4-half .tax-summary-table {
        font-size: 8px;
      }
      .invoice-a4.invoice-a4-half .tax-summary-table th,
      .invoice-a4.invoice-a4-half .tax-summary-table td {
        padding: 1.5px 3px;
      }
      .invoice-a4.invoice-a4-half .final-calc {
        font-size: 9px;
      }
      .invoice-a4.invoice-a4-half .grand-total-row {
        font-size: 11.5px;
        padding: 2px 0;
        margin: 2px 0;
      }
      .invoice-a4.invoice-a4-half .sign-block {
        margin-top: 4px;
        padding: 0 4px;
      }
      .invoice-a4.invoice-a4-half .sign-block div {
        font-size: 8.5px;
        padding-top: 2px;
        width: 140px;
      }
      .invoice-a4.invoice-a4-half .copy-tag {
        font-size: 9px;
        font-weight: 800;
        letter-spacing: 0.5px;
        text-transform: uppercase;
        color: #333;
        text-align: right;
        margin-bottom: 1px;
      }
      .a4-cutting-line {
        border-top: 1px dashed #666;
        margin: 0;
        text-align: center;
        position: relative;
        font-size: 8.5px;
        color: #444;
        letter-spacing: 1.5px;
        page-break-inside: avoid;
        page-break-after: avoid;
        height: 10px;
        line-height: 10px;
      }
      .a4-cutting-line span {
        background: #fff;
        padding: 0 10px;
        position: relative;
        top: -7px;
        font-weight: 700;
      }
      .invoice-a4 .header-block {
        display: flex;
        justify-content: space-between;
        align-items: flex-start;
        border-bottom: 2px solid #000;
        padding-bottom: 8px;
        margin-bottom: 10px;
      }
      .invoice-a4 h1 {
        font-size: 18px;
        margin: 0 0 2px 0;
        font-weight: 900;
      }
      .invoice-a4 .badge-invoice {
        background: #000;
        color: #fff;
        padding: 4px 10px;
        font-weight: 800;
        font-size: 14px;
        display: inline-block;
      }
      .invoice-a4 .meta-grid {
        display: grid;
        grid-template-columns: 1.2fr 1fr;
        gap: 12px;
        border: 1px solid #000;
        padding: 8px;
        margin-bottom: 12px;
      }
      .invoice-a4 table.items-table {
        width: 100%;
        border-collapse: collapse;
        margin-bottom: 12px;
      }
      .invoice-a4 table.items-table th,
      .invoice-a4 table.items-table td {
        border: 1px solid #000;
        padding: 6px 8px;
        font-size: 11.5px;
      }
      .invoice-a4 table.items-table th {
        background-color: #f0f0f0;
        font-weight: 700;
      }
      .invoice-a4 .totals-block {
        display: flex;
        justify-content: space-between;
        align-items: flex-start;
        gap: 20px;
      }
      .invoice-a4 .tax-summary-table {
        width: 55%;
        border-collapse: collapse;
        font-size: 10px;
      }
      .invoice-a4 .tax-summary-table th,
      .invoice-a4 .tax-summary-table td {
        border: 1px solid #666;
        padding: 3px 5px;
        text-align: right;
      }
      .invoice-a4 .tax-summary-table th {
        background: #f7f7f7;
        text-align: center;
      }
      .invoice-a4 .final-calc {
        width: 40%;
        text-align: right;
        font-size: 12px;
      }
      .invoice-a4 .grand-total-row {
        font-size: 16px;
        font-weight: 900;
        border-top: 2px solid #000;
        border-bottom: 2px solid #000;
        padding: 4px 0;
        margin: 6px 0;
      }
      .invoice-a4 .sign-block {
        margin-top: 28px;
        display: flex;
        justify-content: space-between;
        padding: 0 10px;
      }
      .invoice-a4 .sign-block div {
        text-align: center;
        border-top: 1px solid #000;
        padding-top: 6px;
        width: 180px;
      }
      .pdf-actions-bar {
        position: fixed;
        top: 15px;
        right: 15px;
        display: flex;
        gap: 10px;
        z-index: 10000;
      }
      .pdf-btn {
        background: #1b5e20;
        color: #fff;
        padding: 8px 16px;
        border-radius: 6px;
        font-weight: bold;
        cursor: pointer;
        box-shadow: 0 2px 8px rgba(0,0,0,0.25);
        border: none;
        font-size: 13px;
      }
      .pdf-btn:hover {
        background: #144917;
      }
      @media print {
        .pdf-actions-bar,
        .pdf-btn,
        .toast-container,
        .toast-message,
        #toastNotificationContainer {
          display: none !important;
          visibility: hidden !important;
          opacity: 0 !important;
          height: 0 !important;
          width: 0 !important;
        }
      }
    `;
  },

  /**
   * get80mmStyles returns complete self-contained styles for vector 80mm thermal receipts.
   */
  get80mmStyles() {
    return `
      @page {
        size: 80mm auto;
        margin: 2mm 3mm;
      }
      * {
        box-sizing: border-box;
      }
      body {
        margin: 0;
        padding: 0;
        background: #fff;
        color: #000;
        font-family: 'Segoe UI', 'Nirmala UI', Arial, sans-serif;
        -webkit-print-color-adjust: exact;
        print-color-adjust: exact;
      }
      .print-container {
        display: block !important;
        visibility: visible !important;
        width: 100%;
        margin: 0 auto;
        padding: 0;
      }
      .receipt-80mm {
        width: 72mm;
        max-width: 72mm;
        margin: 0 auto;
        padding: 2mm 0;
        font-family: 'Segoe UI', 'Nirmala UI', Arial, sans-serif;
        font-size: 11px;
        line-height: 1.25;
        color: #000;
      }
      .receipt-80mm .center { text-align: center; }
      .receipt-80mm .right { text-align: right; }
      .receipt-80mm .left { text-align: left; }
      .receipt-80mm .bold { font-weight: bold; }
      .receipt-80mm .header-title { font-size: 13.5px; font-weight: 900; }
      .receipt-80mm .header-sub { font-size: 9.5px; margin: 2px 0; }
      .receipt-80mm .dashed-line { border-top: 1px dashed #000; margin: 4px 0; }
      .receipt-80mm table { width: 100%; border-collapse: collapse; font-size: 10.5px; }
      .receipt-80mm th, .receipt-80mm td { padding: 2px 0; vertical-align: top; }
      .receipt-80mm .grand-total-box {
        font-size: 14px;
        font-weight: bold;
        border-top: 1px dashed #000;
        border-bottom: 1px dashed #000;
        padding: 4px 0;
        margin: 4px 0;
      }
      .pdf-actions-bar {
        position: fixed;
        top: 10px;
        right: 10px;
        display: flex;
        gap: 8px;
        z-index: 1000;
      }
      .pdf-btn {
        background: #1b5e20;
        color: #fff;
        padding: 6px 14px;
        border-radius: 4px;
        font-weight: bold;
        cursor: pointer;
        border: none;
        font-size: 12px;
      }
      .pdf-btn:hover {
        background: #2e7d32;
      }
      @media print {
        .pdf-actions-bar,
        .pdf-btn {
          display: none !important;
          visibility: hidden !important;
        }
      }
    `;
  },

  /**
   * saveBillPDF generates a standalone vector invoice ready for saving as PDF or sharing.
   * Supports both '80mm' (bill1) and 'a4' (bill2) templates.
   */
  saveBillPDF(bill, format, settings) {
    if (!bill) {
      alert('No bill data available.');
      return;
    }

    // Support flexible signature: saveBillPDF(bill, settings) or saveBillPDF(bill, format, settings)
    let effFormat = format;
    let s = settings;
    if (typeof format === 'object' && format !== null && typeof settings === 'undefined') {
      s = format;
      effFormat = (s && (s.paperSize || s.defaultPrinter)) || 'a4';
    }
    s = s || {};
    if (!effFormat || effFormat === 'default') {
      effFormat = s.defaultPrinter || s.paperSize || 'a4';
    }
    if (effFormat !== '80mm' && effFormat !== '58mm') {
      effFormat = 'a4';
    }

    const cust = (bill && bill.customer) || {};
    const cleanBillNo = (bill.billNo || 'INVOICE').replace(/[\/\\]/g, '_');
    const cleanCustomer = (cust.name || 'Walkin').replace(/\s+/g, '_');
    const prefix = effFormat === '80mm' ? 'Receipt' : 'Invoice';
    const pdfTitle = `${prefix}_${cleanBillNo}_${cleanCustomer}`;

    const invoiceHtml = effFormat === '80mm' ? PrintEngine.render80mm(bill, s) : PrintEngine.renderA4(bill, s);
    const styles = effFormat === '80mm' ? this.get80mmStyles() : this.getA4Styles();
    const windowGeometry = effFormat === '80mm' ? 'width=460,height=780' : 'width=880,height=920';

    // Try opening dedicated printable preview window
    let win = null;
    try {
      win = window.open('', '_blank', windowGeometry);
    } catch (e) {
      win = null;
    }

    // Check if pop-up was blocked or not allowed in current webview/app mode
    if (!win || win.closed || typeof win.document === 'undefined') {
      const originalTitle = document.title;
      document.title = pdfTitle;
      PrintEngine.print(bill, effFormat, s);
      setTimeout(() => {
        document.title = originalTitle;
      }, 3000);
      return;
    }

    win.document.title = pdfTitle;

    const fullDoc = `
      <!DOCTYPE html>
      <html lang="en">
      <head>
        <meta charset="utf-8">
        <title>${pdfTitle}</title>
        <style>
          ${styles}
        </style>
      </head>
      <body>
        <div class="pdf-actions-bar">
          <button class="pdf-btn" onclick="window.print()">📥 Click to Save as PDF / Print</button>
          <button class="pdf-btn" style="background:#555;" onclick="window.close()">✕ Close</button>
        </div>
        <div id="printContainer" class="print-container">
          ${invoiceHtml}
        </div>
        <script>
          window.addEventListener('DOMContentLoaded', () => {
            setTimeout(() => { window.print(); }, 250);
          });
          setTimeout(() => { window.print(); }, 400);
        </script>
      </body>
      </html>
    `;

    win.document.open();
    win.document.write(fullDoc);
    win.document.close();
  },

  /**
   * saveSalesSummaryPDF generates an executive A4 landscape PDF summary of all sales.
   * Flow: calculates totals -> builds table -> writes landscape template -> triggers print.
   */
  saveSalesSummaryPDF(bills, settings) {
    if (!bills || !bills.length) {
      alert('No sales data to export.');
      return;
    }

    const s = settings || {};
    const pdfTitle = `Sales_Summary_Report_${new Date().toISOString().slice(0, 10)}`;

    const totalSales = bills.reduce((acc, b) => acc + (b.grandTotal || 0), 0);
    const totalGst = bills.filter(b => b.gstOn).reduce((acc, b) => acc + (b.gstAmt || 0), 0);

    const rows = bills.map((b, i) => {
      const cust = (b.customer && b.customer.name) || '-';
      const sub = Number(b.subtotal || 0).toFixed(2);
      const gst = Number(b.gstAmt || 0).toFixed(2);
      const total = Number(b.grandTotal || 0).toFixed(2);
      return `
        <tr>
          <td style="border:1px solid #999; padding:5px 6px; text-align:center;">${i + 1}</td>
          <td style="border:1px solid #999; padding:5px 6px;"><b>${this.esc(b.billNo || '')}</b></td>
          <td style="border:1px solid #999; padding:5px 6px;">${this.esc(b.date || '')}</td>
          <td style="border:1px solid #999; padding:5px 6px;">${this.esc(cust)}</td>
          <td style="border:1px solid #999; padding:5px 6px; text-align:center;">${this.esc(b.payMode || 'Cash')}</td>
          <td style="border:1px solid #999; padding:5px 6px; text-align:right;">₹${sub}</td>
          <td style="border:1px solid #999; padding:5px 6px; text-align:right;">₹${gst}</td>
          <td style="border:1px solid #999; padding:5px 6px; text-align:right; font-weight:bold;">₹${total}</td>
        </tr>
      `;
    }).join('');

    const htmlContent = `
      <!DOCTYPE html>
      <html lang="en">
      <head>
        <meta charset="utf-8">
        <title>${pdfTitle}</title>
        <style>
          @page { size: A4 landscape; margin: 8mm 10mm; }
          * { box-sizing: border-box; }
          body { font-family: 'Segoe UI', Nirmala UI, Arial, sans-serif; padding: 10px; font-size: 11px; color:#000; background:#fff; }
          table { width: 100%; border-collapse: collapse; margin-top: 10px; }
          th { background: #e2e8f0; border: 1px solid #999; padding: 6px; font-size: 11px; text-align: left; }
          .header { text-align: center; border-bottom: 2px solid #1b5e20; padding-bottom: 8px; margin-bottom: 8px; }
          .header h2 { margin: 0 0 4px 0; font-size: 16px; font-weight: 900; }
          .total-box { margin-top: 12px; text-align: right; font-size: 13px; font-weight: bold; border-top: 2px solid #000; padding-top: 6px; }
          .pdf-actions-bar { position: fixed; top: 10px; right: 10px; display: flex; gap: 8px; z-index: 1000; }
          .pdf-btn { background: #1b5e20; color: #fff; padding: 6px 14px; border-radius: 4px; font-weight: bold; cursor: pointer; border: none; font-size: 12px; }
          @media print { .pdf-actions-bar { display: none !important; } }
        </style>
      </head>
      <body>
        <div class="pdf-actions-bar">
          <button class="pdf-btn" onclick="window.print()">📥 Click to Save / Print PDF</button>
          <button class="pdf-btn" style="background:#555;" onclick="window.close()">✕ Close</button>
        </div>
        <div class="header">
          <h2>${this.esc(s.companyName || 'VANUR FARMER PRODUCER COMPANY LIMITED')}</h2>
          <div>SALES REGISTER REPORT • Generated: ${new Date().toLocaleString()}</div>
        </div>
        <table>
          <thead>
            <tr>
              <th style="width:5%; text-align:center;">#</th>
              <th style="width:18%;">Bill No</th>
              <th style="width:12%;">Date</th>
              <th style="width:25%;">Customer</th>
              <th style="width:10%; text-align:center;">Pay Mode</th>
              <th style="width:10%; text-align:right;">Subtotal</th>
              <th style="width:10%; text-align:right;">GST</th>
              <th style="width:10%; text-align:right;">Grand Total</th>
            </tr>
          </thead>
          <tbody>
            ${rows}
          </tbody>
        </table>
        <div class="total-box">
          Total Bills: ${bills.length} &nbsp;|&nbsp; Total GST: ₹${totalGst.toFixed(2)} &nbsp;|&nbsp; Net Sales: ₹${totalSales.toFixed(2)}
        </div>
        <script>
          window.addEventListener('DOMContentLoaded', () => { setTimeout(() => { window.print(); }, 250); });
          setTimeout(() => { window.print(); }, 400);
        </script>
      </body>
      </html>
    `;

    let win = null;
    try {
      win = window.open('', '_blank', 'width=950,height=800');
    } catch (e) {
      win = null;
    }

    if (!win || win.closed || typeof win.document === 'undefined') {
      // In-page fallback
      const originalTitle = document.title;
      document.title = pdfTitle;
      const container = document.getElementById('printContainer');
      if (container) {
        container.innerHTML = htmlContent;
        setTimeout(() => {
          window.print();
          document.title = originalTitle;
        }, 200);
      }
      return;
    }

    win.document.title = pdfTitle;
    win.document.open();
    win.document.write(htmlContent);
    win.document.close();
  }
};
