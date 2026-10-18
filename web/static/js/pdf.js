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
     2. saveSalesSummaryPDF(bills, settings):
        - Formats the complete sales register into an A4 landscape executive report.
        - Summarizes net turnover, total GST collected, and individual bill statuses.
        - Triggers instant PDF printing.

   FLOW:
     Bill Object -> HTML Document Template -> Styled Browser Window -> Trigger Print / Save PDF
   ========================================================================== */

const PDFEngine = {
  /**
   * saveBillPDF generates a standalone vector invoice ready for saving as PDF or sharing.
   * Flow: constructs document title -> opens window -> writes A4 template -> triggers print.
   */
  saveBillPDF(bill, settings) {
    if (!bill) {
      alert('No bill data available.');
      return;
    }

    const s = settings || {};
    const win = window.open('', '_blank', 'width=850,height=900');
    if (!win) {
      alert('Pop-up blocked! Please allow pop-ups to save PDF.');
      return;
    }

    const cleanBillNo = (bill.billNo || 'INVOICE').replace(/[\/\\]/g, '_');
    const cleanCustomer = (bill.customer.name || 'Walkin').replace(/\s+/g, '_');
    const pdfTitle = `Invoice_${cleanBillNo}_${cleanCustomer}`;
    win.document.title = pdfTitle;

    const htmlContent = `
      <!DOCTYPE html>
      <html>
      <head>
        <meta charset="utf-8">
        <title>${pdfTitle}</title>
        <link rel="stylesheet" href="/css/print.css">
        <style>
          body {
            margin: 0;
            padding: 20px;
            font-family: 'Segoe UI', Nirmala UI, Arial, sans-serif;
            background: #fff;
            color: #000;
          }
          @page {
            size: A4 portrait;
            margin: 10mm;
          }
          .pdf-actions {
            position: fixed;
            top: 15px;
            right: 15px;
            background: #1b5e20;
            color: #fff;
            padding: 8px 16px;
            border-radius: 6px;
            font-weight: bold;
            cursor: pointer;
            box-shadow: 0 2px 8px rgba(0,0,0,0.2);
            border: none;
            z-index: 1000;
          }
          @media print {
            .pdf-actions { display: none !important; }
          }
        </style>
      </head>
      <body>
        <button class="pdf-actions" onclick="window.print()">📥 Click to Save / Print PDF</button>
        ${PrintEngine.renderA4(bill, s)}
        <script>
          setTimeout(() => {
            window.print();
          }, 400);
        </script>
      </body>
      </html>
    `;

    win.document.write(htmlContent);
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
    const win = window.open('', '_blank', 'width=900,height=900');
    if (!win) return;

    const pdfTitle = `Sales_Summary_Report_${new Date().toISOString().slice(0, 10)}`;
    win.document.title = pdfTitle;

    const totalSales = bills.reduce((acc, b) => acc + (b.grandTotal || 0), 0);
    const totalGst = bills.filter(b => b.gstOn).reduce((acc, b) => acc + (b.gstAmt || 0), 0);

    const rows = bills.map((b, i) => `
      <tr>
        <td style="border:1px solid #ddd; padding:6px; text-align:center;">${i + 1}</td>
        <td style="border:1px solid #ddd; padding:6px;"><b>${b.billNo}</b></td>
        <td style="border:1px solid #ddd; padding:6px;">${b.date}</td>
        <td style="border:1px solid #ddd; padding:6px;">${b.customer.name}</td>
        <td style="border:1px solid #ddd; padding:6px; text-align:center;">${b.payMode}</td>
        <td style="border:1px solid #ddd; padding:6px; text-align:right;">₹${b.subtotal.toFixed(2)}</td>
        <td style="border:1px solid #ddd; padding:6px; text-align:right;">₹${b.gstAmt.toFixed(2)}</td>
        <td style="border:1px solid #ddd; padding:6px; text-align:right; font-weight:bold;">₹${b.grandTotal.toFixed(2)}</td>
      </tr>
    `).join('');

    const html = `
      <!DOCTYPE html>
      <html>
      <head>
        <meta charset="utf-8">
        <title>${pdfTitle}</title>
        <style>
          body { font-family: sans-serif; padding: 20px; font-size: 12px; }
          table { width: 100%; border-collapse: collapse; margin-top: 15px; }
          th { background: #f0f0f0; border: 1px solid #ddd; padding: 8px; text-align: left; }
          .header { text-align: center; border-bottom: 2px solid #1b5e20; padding-bottom: 10px; }
          .total-box { margin-top: 15px; text-align: right; font-size: 14px; font-weight: bold; }
          @page { size: A4 landscape; margin: 10mm; }
        </style>
      </head>
      <body>
        <div class="header">
          <h2>${s.companyName || 'VANUR FARMER PRODUCER COMPANY LTD'}</h2>
          <div>SALES REGISTER REPORT • Generated: ${new Date().toLocaleString()}</div>
        </div>
        <table>
          <thead>
            <tr>
              <th>#</th>
              <th>Bill No</th>
              <th>Date</th>
              <th>Customer</th>
              <th>Pay Mode</th>
              <th style="text-align:right;">Subtotal</th>
              <th style="text-align:right;">GST</th>
              <th style="text-align:right;">Grand Total</th>
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
          setTimeout(() => { window.print(); }, 400);
        </script>
      </body>
      </html>
    `;

    win.document.write(html);
    win.document.close();
  }
};
