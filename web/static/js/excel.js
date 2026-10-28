/* ==========================================================================
   Siraazhi Pro POS — Multi-Format Excel (.xlsx) & CSV Export Engine
   
   WHAT IT IS:
     Client-side spreadsheet generation engine utilizing SheetJS (XLSX).
     Generates true multi-tab .xlsx workbooks and raw CSV files directly
     in the browser without requiring any server-side Microsoft Office or COM tools.

   HOW IT WORKS:
     1. exportSalesToExcel(bills):
        - Creates a new XLSX virtual workbook.
        - Sheet 1 ('Sales Register'): Flattens bills into invoice-level rows with
          taxable, CGST, SGST, IGST, subtotal, and payment mode columns.
        - Sheet 2 ('Item-Wise Sales'): Flattens every individual product sale line
          for detailed margin and volume analysis.
        - Triggers immediate browser download of the .xlsx file.
     2. exportGSTR1ToExcel(gstr1Data):
        - Packages GSTR-1 tax compliance tables into 3 official tabs:
          'B2B Invoices', 'B2C Small Summary', and 'HSN Summary'.
        - Ready for immediate hand-off to the FPO's Chartered Accountant.
     3. downloadCSV():
        - Directly streams a raw CSV file from the backend API for Tally import.

   FLOW:
     User clicks Export -> JavaScript extracts data array -> SheetJS builds XLSX -> Auto-Download
   ========================================================================== */

const ExportEngine = {
  /**
   * exportSalesToExcel exports the full sales register to a multi-sheet .xlsx workbook.
   * Flow: iterates bills -> creates Sheet 1 (bills) and Sheet 2 (items) -> writes XLSX -> triggers download.
   */
  exportSalesToExcel(bills) {
    if (!bills || !bills.length) {
      alert('No sales data available to export.');
      return;
    }

    if (typeof XLSX === 'undefined') {
      alert('Excel library loading... Please check internet or try CSV export.');
      return;
    }

    // Sheet 1: Sales Summary (Bill by Bill)
    const salesData = bills.map(b => ({
      'Bill No': b.billNo,
      'Date': b.date,
      'Customer Name': b.customer.name || 'Walk-in',
      'Customer Phone': b.customer.phone || '',
      'Customer GSTIN': b.customer.gstin || '',
      'Type': b.gstOn ? 'GST Tax Invoice' : 'Cash Bill',
      'Items Count': b.items.length,
      'Subtotal (₹)': Number(b.subtotal.toFixed(2)),
      'Discount (₹)': Number(b.discount.toFixed(2)),
      'Taxable Value (₹)': Number(b.taxable.toFixed(2)),
      'CGST (₹)': Number(b.cgst.toFixed(2)),
      'SGST (₹)': Number(b.sgst.toFixed(2)),
      'IGST (₹)': Number(b.igst.toFixed(2)),
      'Total GST (₹)': Number(b.gstAmt.toFixed(2)),
      'Grand Total (₹)': Number(b.grandTotal.toFixed(2)),
      'Payment Mode': b.payMode,
      'Paid (₹)': Number(b.paid.toFixed(2)),
      'Balance (₹)': Number(b.balance.toFixed(2)),
      'Notes': b.note || ''
    }));

    // Sheet 2: Item-wise Line Sales
    const itemData = [];
    bills.forEach(b => {
      b.items.forEach(it => {
        itemData.push({
          'Bill No': b.billNo,
          'Date': b.date,
          'Product Name': it.nameEn,
          'Product (Tamil)': it.nameTa || '',
          'HSN Code': it.hsn || '',
          'Quantity Sold': it.qty,
          'Unit Price (₹)': Number(it.price.toFixed(2)),
          'Line Amount (₹)': Number(it.amount.toFixed(2)),
          'GST %': it.gstPct,
          'GST Portion (₹)': Number(it.gstPortion.toFixed(2)),
          'Customer': b.customer.name || 'Walk-in',
          'Pay Mode': b.payMode
        });
      });
    });

    const wb = XLSX.utils.book_new();
    const wsSales = XLSX.utils.json_to_sheet(salesData);
    const wsItems = XLSX.utils.json_to_sheet(itemData);

    XLSX.utils.book_append_sheet(wb, wsSales, 'Sales Register');
    XLSX.utils.book_append_sheet(wb, wsItems, 'Item-Wise Sales');

    const fileName = `Siraazhi_Sales_Register_${new Date().toISOString().slice(0, 10)}.xlsx`;
    XLSX.writeFile(wb, fileName);
  },

  /**
   * exportGSTR1ToExcel creates official Indian GST tax return spreadsheets.
   * Flow: formats B2B table, B2C Small table, and HSN table -> appends sheets -> downloads XLSX.
   */
  exportGSTR1ToExcel(gstr1Data) {
    if (!gstr1Data) {
      alert('No GST report data available.');
      return;
    }

    if (typeof XLSX === 'undefined') {
      alert('Excel library not loaded.');
      return;
    }

    const wb = XLSX.utils.book_new();

    // B2B Sheet
    const b2bSheetData = (gstr1Data.b2b || []).map(row => ({
      'Customer GSTIN': row.customerGstin,
      'Customer Legal Name': row.customerName,
      'Invoice Number': row.invoiceNo,
      'Invoice Date': row.date,
      'Invoice Value (₹)': row.invoiceValue,
      'Taxable Value (₹)': row.taxableValue,
      'CGST Amount (₹)': row.cgst,
      'SGST Amount (₹)': row.sgst,
      'IGST Amount (₹)': row.igst
    }));
    const wsB2B = XLSX.utils.json_to_sheet(b2bSheetData);
    XLSX.utils.book_append_sheet(wb, wsB2B, 'B2B Invoices');

    // B2C Small Sheet
    const b2cSheetData = (gstr1Data.b2cSmall || []).map(row => ({
      'Rate %': row.taxRate,
      'Taxable Value (₹)': row.taxableValue,
      'CGST (₹)': row.cgst,
      'SGST (₹)': row.sgst,
      'Total Consumer Sales (₹)': row.totalAmount
    }));
    const wsB2C = XLSX.utils.json_to_sheet(b2cSheetData);
    XLSX.utils.book_append_sheet(wb, wsB2C, 'B2C Small Summary');

    // HSN Sheet
    const hsnSheetData = (gstr1Data.hsnSummary || []).map(row => ({
      'HSN Code': row.hsn,
      'Description': row.description,
      'Total Quantity Sold': row.qty,
      'Total Invoice Value (₹)': row.totalValue,
      'Taxable Value (₹)': row.taxableValue,
      'CGST (₹)': row.cgst,
      'SGST (₹)': row.sgst,
      'IGST (₹)': row.igst
    }));
    const wsHSN = XLSX.utils.json_to_sheet(hsnSheetData);
    XLSX.utils.book_append_sheet(wb, wsHSN, 'HSN Summary');

    const fileName = `Siraazhi_GSTR1_CA_Report_${new Date().toISOString().slice(0, 10)}.xlsx`;
    XLSX.writeFile(wb, fileName);
  },

  /**
   * exportMonthlyPnLToExcel creates an executive Monthly Income & Expense Excel workbook
   * matching the exact multi-section format of the VFPCL Purchase & Sales register.
   */
  exportMonthlyPnLToExcel(monthStr, bills, purchases, expenses, settings) {
    if (typeof XLSX === 'undefined') {
      alert('Excel library not loaded.');
      return;
    }

    const s = settings || {};
    let totalSalesCash = 0;
    let totalSalesBank = 0;
    let totalPurchasesCash = 0;
    let totalPurchasesBank = 0;
    let totalExpensesCash = 0;
    let totalExpensesBank = 0;

    // 1. Process Sales Line Items
    const salesRows = [];
    (bills || []).forEach(b => {
      let bCash = 0;
      let bBank = 0;
      if (b.payMode === 'Split' || (b.cashPaid > 0 && b.upiPaid > 0)) {
        bCash = Number(b.cashPaid) || 0;
        bBank = Number(b.upiPaid) || 0;
      } else if (b.payMode === 'UPI' || b.payMode === 'Card') {
        bBank = Number(b.paid) || Number(b.grandTotal) || 0;
      } else {
        bCash = Number(b.paid) || Number(b.grandTotal) || 0;
      }
      totalSalesCash += bCash;
      totalSalesBank += bBank;

      if (b.items && b.items.length > 0) {
        b.items.forEach(it => {
          const ratio = b.grandTotal > 0 ? (it.amount / b.grandTotal) : (1 / b.items.length);
          const itCash = Math.round(bCash * ratio * 100) / 100;
          const itBank = Math.round(bBank * ratio * 100) / 100;

          salesRows.push({
            'DATE': b.date,
            'INVOICE NO': b.billNo,
            'DESCRIPTIVE OF GOODS': it.nameEn + (it.nameTa ? ` (${it.nameTa})` : ''),
            'QUANTITY': it.qty,
            'UNIT RATE (₹)': Number(it.price.toFixed(2)),
            'CASH (₹)': itCash > 0 ? itCash : '',
            'BANK (₹)': itBank > 0 ? itBank : '',
            'TOTAL (₹)': Number(it.amount.toFixed(2)),
            'CUSTOMER': b.customer ? (b.customer.name || 'Consumer') : 'Consumer'
          });
        });
      } else {
        salesRows.push({
          'DATE': b.date,
          'INVOICE NO': b.billNo,
          'DESCRIPTIVE OF GOODS': 'General Counter Sales',
          'QUANTITY': 1,
          'UNIT RATE (₹)': Number(b.grandTotal.toFixed(2)),
          'CASH (₹)': bCash > 0 ? bCash : '',
          'BANK (₹)': bBank > 0 ? bBank : '',
          'TOTAL (₹)': Number(b.grandTotal.toFixed(2)),
          'CUSTOMER': b.customer ? (b.customer.name || 'Consumer') : 'Consumer'
        });
      }
    });

    // 2. Process Purchases
    const purchaseRows = (purchases || []).map(p => {
      const pCost = Number(p.cost) || 0;
      const isCash = (p.note && p.note.toLowerCase().includes('cash'));
      const pCash = isCash ? pCost : 0;
      const pBank = isCash ? 0 : pCost;
      totalPurchasesCash += pCash;
      totalPurchasesBank += pBank;

      return {
        'DATE': p.date,
        'SUPPLIER': p.supplier || 'Farmer / Vendor',
        'MATERIAL / ITEM': p.item,
        'QUANTITY': p.qty || '',
        'UNIT': p.unit || 'kg',
        'CASH (₹)': pCash > 0 ? pCash : '',
        'BANK (₹)': pBank > 0 ? pBank : '',
        'TOTAL COST (₹)': pCost,
        'NOTES': p.note || ''
      };
    });

    // 3. Process Expenses
    const expenseRows = (expenses || []).map(e => {
      const eAmt = Number(e.amount) || 0;
      const isCash = (e.note && e.note.toLowerCase().includes('cash')) || (e.head && e.head.toLowerCase().includes('cash'));
      const eCash = isCash ? eAmt : 0;
      const eBank = isCash ? 0 : eAmt;
      totalExpensesCash += eCash;
      totalExpensesBank += eBank;

      return {
        'DATE': e.date,
        'EXPENSE HEAD': e.head || 'Operating Expense',
        'PAYEE / PURPOSE': e.note || e.head,
        'CASH (₹)': eCash > 0 ? eCash : '',
        'BANK (₹)': eBank > 0 ? eBank : '',
        'TOTAL AMOUNT (₹)': eAmt
      };
    });

    // 4. Executive Reconciliation Summary (Exact VFPCL Format)
    const totalSales = totalSalesCash + totalSalesBank;
    const totalPurchases = totalPurchasesCash + totalPurchasesBank;
    const totalExpenses = totalExpensesCash + totalExpensesBank;
    const totalOutflowCash = totalPurchasesCash + totalExpensesCash;
    const totalOutflowBank = totalPurchasesBank + totalExpensesBank;
    const totalOutflow = totalPurchases + totalExpenses;
    const netSurplusCash = totalSalesCash - totalOutflowCash;
    const netSurplusBank = totalSalesBank - totalOutflowBank;
    const netSurplusTotal = totalSales - totalOutflow;

    const summaryRows = [
      { 'FINANCIAL ACCOUNTING HEAD': '1. GROSS SALES REVENUE (INCOME)', 'CASH (₹)': totalSalesCash, 'BANK (₹)': totalSalesBank, 'TOTAL (₹)': totalSales },
      { 'FINANCIAL ACCOUNTING HEAD': '2. RAW MATERIAL PURCHASES (COGS)', 'CASH (₹)': totalPurchasesCash, 'BANK (₹)': totalPurchasesBank, 'TOTAL (₹)': totalPurchases },
      { 'FINANCIAL ACCOUNTING HEAD': '3. OPERATIONAL & CAPITAL EXPENSES', 'CASH (₹)': totalExpensesCash, 'BANK (₹)': totalExpensesBank, 'TOTAL (₹)': totalExpenses },
      { 'FINANCIAL ACCOUNTING HEAD': '---------------------------------------', 'CASH (₹)': '------------', 'BANK (₹)': '------------', 'TOTAL (₹)': '------------' },
      { 'FINANCIAL ACCOUNTING HEAD': 'TOTAL GROSS OUTFLOW (2 + 3)', 'CASH (₹)': totalOutflowCash, 'BANK (₹)': totalOutflowBank, 'TOTAL (₹)': totalOutflow },
      { 'FINANCIAL ACCOUNTING HEAD': 'NET OPERATIONAL CASH FLOW (SURPLUS)', 'CASH (₹)': netSurplusCash, 'BANK (₹)': netSurplusBank, 'TOTAL (₹)': netSurplusTotal }
    ];

    const wb = XLSX.utils.book_new();
    const wsSummary = XLSX.utils.json_to_sheet(summaryRows);
    XLSX.utils.book_append_sheet(wb, wsSummary, 'Executive Summary');

    const wsSales = XLSX.utils.json_to_sheet(salesRows);
    XLSX.utils.book_append_sheet(wb, wsSales, 'Sales Register');

    const wsPurchases = XLSX.utils.json_to_sheet(purchaseRows);
    XLSX.utils.book_append_sheet(wb, wsPurchases, 'Purchases');

    const wsExpenses = XLSX.utils.json_to_sheet(expenseRows);
    XLSX.utils.book_append_sheet(wb, wsExpenses, 'Expenses');

    const cleanMonth = (monthStr || new Date().toISOString().slice(0, 7));
    const fileName = `Siraazhi_Monthly_Income_Expense_${cleanMonth}.xlsx`;
    XLSX.writeFile(wb, fileName);
  },

  /**
   * downloadCSV opens the server CSV endpoint for immediate streaming download.
   */
  downloadCSV(bills) {
    window.open('/api/export/csv', '_blank');
  }
};

window.ExportEngine = ExportEngine;
