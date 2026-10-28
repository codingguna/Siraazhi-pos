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

    // Sheet 1: Item-Wise Sales Summary (Detailed per-product aggregation)
    const totalSales = (bills || []).reduce((sum, b) => sum + (Number(b.grandTotal) || 0), 0);
    const wsItemSummary = XLSX.utils.json_to_sheet(this.buildItemWiseSalesRows(bills, totalSales));

    // Sheet 2: Sales Summary (Bill by Bill)
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

    // Sheet 3: Item-wise Line Sales
    const itemData = [];
    bills.forEach(b => {
      b.items.forEach(it => {
        itemData.push({
          'Bill No': b.billNo,
          'Date': b.date,
          'Product Name': it.nameEn,
          'Product (Tamil)': it.nameTa || '',
          'Category': this.getItemCategory(it),
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

    XLSX.utils.book_append_sheet(wb, wsItemSummary, 'Item-Wise Summary');
    XLSX.utils.book_append_sheet(wb, wsSales, 'Sales Register');
    XLSX.utils.book_append_sheet(wb, wsItems, 'Item-Wise Lines');

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
   * getItemCategory identifies the primary product family for any line item SKU.
   */
  getItemCategory(item) {
    const nameEn = (item.nameEn || '').toLowerCase();
    const nameTa = (item.nameTa || '').toLowerCase();
    const cat = (item.category || '').toLowerCase();
    const combined = `${nameEn} ${nameTa} ${cat}`;

    if (
      combined.includes('cake') ||
      combined.includes('pinac') ||
      combined.includes('punnac') ||
      combined.includes('புண்ணாக்கு') ||
      combined.includes('பிண்ணாக்கு')
    ) {
      return 'Oil Cakes (Cattle Feed)';
    }
    if (
      (combined.includes('seed') || combined.includes('விதை') || combined.includes('பருப்பு') || combined.includes('pod') || combined.includes('kernel')) &&
      !combined.includes('oil') && !combined.includes('எண்ணெய்')
    ) {
      return 'Seeds & Raw Materials';
    }
    if (
      combined.includes('groundnut') ||
      combined.includes('kadalai') ||
      combined.includes('kadala') ||
      combined.includes('நிலக்கடலை') ||
      combined.includes('கடலை') ||
      combined.includes('peanut')
    ) {
      return 'Groundnut Oil';
    }
    if (
      combined.includes('sesame') ||
      combined.includes('gingelly') ||
      combined.includes('nalla') ||
      combined.includes('நல்லெண்ணெய்') ||
      combined.includes('எள்') ||
      combined.includes('ellu') ||
      combined.includes('til')
    ) {
      return 'Sesame Oil';
    }
    if (
      combined.includes('coconut') ||
      combined.includes('thengai') ||
      combined.includes('தேங்காய்') ||
      combined.includes('copra')
    ) {
      return 'Coconut Oil';
    }
    if (
      combined.includes('lamp') ||
      combined.includes('deepam') ||
      combined.includes('vilaku') ||
      combined.includes('vilakku') ||
      combined.includes('விளக்கு') ||
      combined.includes('பூஜை') ||
      combined.includes('pooja')
    ) {
      return 'Lamp Oil (Deepam)';
    }
    return 'Other Items';
  },

  /**
   * computeCategorySummary calculates aggregated sales by major product category.
   */
  computeCategorySummary(bills) {
    const catMap = {
      'Groundnut Oil': { name: 'Groundnut Oil (கடலை எண்ணெய்)', icon: '🥜', unit: 'L', qty: 0, cash: 0, bank: 0, total: 0 },
      'Sesame Oil': { name: 'Sesame Oil (நல்லெண்ணெய்)', icon: '🫘', unit: 'L', qty: 0, cash: 0, bank: 0, total: 0 },
      'Coconut Oil': { name: 'Coconut Oil (தேங்காய் எண்ணெய்)', icon: '🥥', unit: 'L', qty: 0, cash: 0, bank: 0, total: 0 },
      'Oil Cakes (Cattle Feed)': { name: 'Oil Cakes (புண்ணாக்கு / By-Products)', icon: '🌾', unit: 'kg', qty: 0, cash: 0, bank: 0, total: 0 },
      'Lamp Oil (Deepam)': { name: 'Lamp Oil (விளக்கு எண்ணெய்)', icon: '🪔', unit: 'L', qty: 0, cash: 0, bank: 0, total: 0 },
      'Seeds & Raw Materials': { name: 'Seeds & Raw Materials', icon: '🌱', unit: 'kg', qty: 0, cash: 0, bank: 0, total: 0 },
      'Other Items': { name: 'Other Items & Sundry Sales', icon: '📦', unit: 'units', qty: 0, cash: 0, bank: 0, total: 0 }
    };

    let grandCash = 0;
    let grandBank = 0;
    let grandTotal = 0;
    let grandQty = 0;

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
      grandCash += bCash;
      grandBank += bBank;
      grandTotal += (bCash + bBank);

      if (b.items && b.items.length) {
        b.items.forEach(it => {
          const ratio = b.grandTotal > 0 ? (it.amount / b.grandTotal) : (1 / b.items.length);
          const itCash = Math.round(bCash * ratio * 100) / 100;
          const itBank = Math.round(bBank * ratio * 100) / 100;
          const cat = this.getItemCategory(it);
          if (!catMap[cat]) {
            catMap[cat] = { name: cat, icon: '📦', unit: 'units', qty: 0, cash: 0, bank: 0, total: 0 };
          }
          catMap[cat].qty += Number(it.qty) || 0;
          catMap[cat].cash += itCash;
          catMap[cat].bank += itBank;
          catMap[cat].total += Number(it.amount) || 0;
          grandQty += Number(it.qty) || 0;
        });
      } else {
        const cat = 'Other Items';
        catMap[cat].qty += 1;
        catMap[cat].cash += bCash;
        catMap[cat].bank += bBank;
        catMap[cat].total += Number(b.grandTotal) || 0;
        grandQty += 1;
      }
    });

    const categories = [];
    const order = ['Groundnut Oil', 'Sesame Oil', 'Coconut Oil', 'Oil Cakes (Cattle Feed)', 'Lamp Oil (Deepam)', 'Seeds & Raw Materials', 'Other Items'];

    order.forEach(catKey => {
      const d = catMap[catKey];
      if (d && (d.total > 0 || d.qty > 0)) {
        const pct = grandTotal > 0 ? ((d.total / grandTotal) * 100).toFixed(2) : '0.00';
        categories.push({
          key: catKey,
          name: d.name,
          icon: d.icon,
          unit: d.unit,
          qty: d.qty,
          cash: d.cash,
          bank: d.bank,
          total: d.total,
          pct: pct,
          isTotal: false
        });
      }
    });

    for (const [k, d] of Object.entries(catMap)) {
      if (!order.includes(k) && (d.total > 0 || d.qty > 0)) {
        const pct = grandTotal > 0 ? ((d.total / grandTotal) * 100).toFixed(2) : '0.00';
        categories.push({
          key: k,
          name: d.name,
          icon: d.icon,
          unit: d.unit,
          qty: d.qty,
          cash: d.cash,
          bank: d.bank,
          total: d.total,
          pct: pct,
          isTotal: false
        });
      }
    }

    categories.push({
      key: 'TOTAL',
      name: 'TOTAL GROSS SALES',
      icon: '📊',
      unit: '',
      qty: grandQty,
      cash: grandCash,
      bank: grandBank,
      total: grandTotal,
      pct: '100.00',
      isTotal: true
    });

    return {
      categories,
      grandCash,
      grandBank,
      grandTotal,
      grandQty
    };
  },

  /**
   * buildItemWiseSalesRows formats detailed item rows with category subtotals.
   */
  buildItemWiseSalesRows(bills, grandTotalSales) {
    const itemMap = {};
    const catTotals = {};

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

      if (b.items && b.items.length) {
        b.items.forEach(it => {
          const ratio = b.grandTotal > 0 ? (it.amount / b.grandTotal) : (1 / b.items.length);
          const itCash = Math.round(bCash * ratio * 100) / 100;
          const itBank = Math.round(bBank * ratio * 100) / 100;
          const cat = this.getItemCategory(it);
          const key = it.nameEn || 'General Item';

          if (!itemMap[cat]) itemMap[cat] = {};
          if (!itemMap[cat][key]) {
            itemMap[cat][key] = {
              nameEn: it.nameEn,
              nameTa: it.nameTa || '',
              hsn: it.hsn || '',
              qty: 0,
              cash: 0,
              bank: 0,
              total: 0
            };
          }
          itemMap[cat][key].qty += Number(it.qty) || 0;
          itemMap[cat][key].cash += itCash;
          itemMap[cat][key].bank += itBank;
          itemMap[cat][key].total += Number(it.amount) || 0;

          if (!catTotals[cat]) catTotals[cat] = { qty: 0, cash: 0, bank: 0, total: 0 };
          catTotals[cat].qty += Number(it.qty) || 0;
          catTotals[cat].cash += itCash;
          catTotals[cat].bank += itBank;
          catTotals[cat].total += Number(it.amount) || 0;
        });
      } else {
        const cat = 'Other Items';
        const key = 'General Counter Sales';
        if (!itemMap[cat]) itemMap[cat] = {};
        if (!itemMap[cat][key]) {
          itemMap[cat][key] = {
            nameEn: key,
            nameTa: '',
            hsn: '',
            qty: 0,
            cash: 0,
            bank: 0,
            total: 0
          };
        }
        itemMap[cat][key].qty += 1;
        itemMap[cat][key].cash += bCash;
        itemMap[cat][key].bank += bBank;
        itemMap[cat][key].total += Number(b.grandTotal) || 0;

        if (!catTotals[cat]) catTotals[cat] = { qty: 0, cash: 0, bank: 0, total: 0 };
        catTotals[cat].qty += 1;
        catTotals[cat].cash += bCash;
        catTotals[cat].bank += bBank;
        catTotals[cat].total += Number(b.grandTotal) || 0;
      }
    });

    const rows = [];
    const order = ['Groundnut Oil', 'Sesame Oil', 'Coconut Oil', 'Oil Cakes (Cattle Feed)', 'Lamp Oil (Deepam)', 'Seeds & Raw Materials', 'Other Items'];

    let overallQty = 0;
    let overallCash = 0;
    let overallBank = 0;
    let overallTotal = 0;

    const allCats = Object.keys(itemMap);
    const sortedCats = [
      ...order.filter(c => allCats.includes(c)),
      ...allCats.filter(c => !order.includes(c))
    ];

    sortedCats.forEach(cat => {
      const items = itemMap[cat];
      const cTot = catTotals[cat] || { qty: 0, cash: 0, bank: 0, total: 0 };
      overallQty += cTot.qty;
      overallCash += cTot.cash;
      overallBank += cTot.bank;
      overallTotal += cTot.total;

      // Header row for category
      rows.push({
        'PRODUCT CATEGORY': `>>> ${cat.toUpperCase()} <<<`,
        'ITEM NAME (ENGLISH)': '',
        'ITEM NAME (TAMIL)': '',
        'HSN CODE': '',
        'TOTAL QUANTITY SOLD': '',
        'AVERAGE RATE (₹)': '',
        'CASH SALES (₹)': '',
        'BANK / UPI SALES (₹)': '',
        'TOTAL SALES VALUE (₹)': '',
        '% SHARE OF TOTAL SALES': ''
      });

      // Item rows sorted descending by total revenue
      const sortedItems = Object.values(items).sort((a, b) => b.total - a.total);
      sortedItems.forEach(it => {
        const avg = it.qty > 0 ? (it.total / it.qty) : 0;
        const pct = grandTotalSales > 0 ? ((it.total / grandTotalSales) * 100).toFixed(2) : '0.00';
        rows.push({
          'PRODUCT CATEGORY': cat,
          'ITEM NAME (ENGLISH)': it.nameEn,
          'ITEM NAME (TAMIL)': it.nameTa,
          'HSN CODE': it.hsn,
          'TOTAL QUANTITY SOLD': Number(it.qty.toFixed(2)),
          'AVERAGE RATE (₹)': Number(avg.toFixed(2)),
          'CASH SALES (₹)': Number(it.cash.toFixed(2)),
          'BANK / UPI SALES (₹)': Number(it.bank.toFixed(2)),
          'TOTAL SALES VALUE (₹)': Number(it.total.toFixed(2)),
          '% SHARE OF TOTAL SALES': `${pct}%`
        });
      });

      // Subtotal row for category
      const catPct = grandTotalSales > 0 ? ((cTot.total / grandTotalSales) * 100).toFixed(2) : '0.00';
      const catAvg = cTot.qty > 0 ? (cTot.total / cTot.qty) : 0;
      rows.push({
        'PRODUCT CATEGORY': `SUBTOTAL — ${cat.toUpperCase()}`,
        'ITEM NAME (ENGLISH)': `${sortedItems.length} Products`,
        'ITEM NAME (TAMIL)': '',
        'HSN CODE': '---',
        'TOTAL QUANTITY SOLD': Number(cTot.qty.toFixed(2)),
        'AVERAGE RATE (₹)': Number(catAvg.toFixed(2)),
        'CASH SALES (₹)': Number(cTot.cash.toFixed(2)),
        'BANK / UPI SALES (₹)': Number(cTot.bank.toFixed(2)),
        'TOTAL SALES VALUE (₹)': Number(cTot.total.toFixed(2)),
        '% SHARE OF TOTAL SALES': `${catPct}%`
      });

      rows.push({});
    });

    rows.push({
      'PRODUCT CATEGORY': 'GRAND TOTAL MONTHLY SALES',
      'ITEM NAME (ENGLISH)': 'ALL PRODUCT CATEGORIES',
      'ITEM NAME (TAMIL)': 'அனைத்து பொருட்கள்',
      'HSN CODE': '---',
      'TOTAL QUANTITY SOLD': Number(overallQty.toFixed(2)),
      'AVERAGE RATE (₹)': overallQty > 0 ? Number((overallTotal / overallQty).toFixed(2)) : '---',
      'CASH SALES (₹)': Number(overallCash.toFixed(2)),
      'BANK / UPI SALES (₹)': Number(overallBank.toFixed(2)),
      'TOTAL SALES VALUE (₹)': Number(overallTotal.toFixed(2)),
      '% SHARE OF TOTAL SALES': '100.00%'
    });

    return rows;
  },

  /**
   * buildGroupedSalesRegisterRows groups sales lines into category sections with subtotals.
   */
  buildGroupedSalesRegisterRows(bills) {
    const catSales = {};
    const order = ['Groundnut Oil', 'Sesame Oil', 'Coconut Oil', 'Oil Cakes (Cattle Feed)', 'Lamp Oil (Deepam)', 'Seeds & Raw Materials', 'Other Items'];

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

      if (b.items && b.items.length) {
        b.items.forEach(it => {
          const ratio = b.grandTotal > 0 ? (it.amount / b.grandTotal) : (1 / b.items.length);
          const itCash = Math.round(bCash * ratio * 100) / 100;
          const itBank = Math.round(bBank * ratio * 100) / 100;
          const cat = this.getItemCategory(it);
          if (!catSales[cat]) catSales[cat] = [];
          catSales[cat].push({
            date: b.date,
            billNo: b.billNo,
            desc: it.nameEn + (it.nameTa ? ` (${it.nameTa})` : ''),
            qty: it.qty,
            rate: it.price,
            cash: itCash,
            bank: itBank,
            total: it.amount,
            customer: b.customer ? (b.customer.name || 'Walk-in') : 'Walk-in'
          });
        });
      } else {
        const cat = 'Other Items';
        if (!catSales[cat]) catSales[cat] = [];
        catSales[cat].push({
          date: b.date,
          billNo: b.billNo,
          desc: 'General Counter Sales',
          qty: 1,
          rate: b.grandTotal,
          cash: bCash,
          bank: bBank,
          total: b.grandTotal,
          customer: b.customer ? (b.customer.name || 'Walk-in') : 'Walk-in'
        });
      }
    });

    const rows = [];
    let grandQty = 0;
    let grandCash = 0;
    let grandBank = 0;
    let grandTotal = 0;

    const allCats = Object.keys(catSales);
    const sortedCats = [
      ...order.filter(c => allCats.includes(c)),
      ...allCats.filter(c => !order.includes(c))
    ];

    sortedCats.forEach(cat => {
      const list = catSales[cat] || [];
      if (!list.length) return;

      let cQty = 0, cCash = 0, cBank = 0, cTotal = 0;
      rows.push({
        'DATE': `=== ${cat.toUpperCase()} SALES ===`,
        'INVOICE NO': '',
        'DESCRIPTIVE OF GOODS': '',
        'QUANTITY': '',
        'UNIT RATE (₹)': '',
        'CASH (₹)': '',
        'BANK (₹)': '',
        'TOTAL (₹)': '',
        'CUSTOMER': ''
      });

      list.forEach(r => {
        cQty += r.qty;
        cCash += r.cash;
        cBank += r.bank;
        cTotal += r.total;

        rows.push({
          'DATE': r.date,
          'INVOICE NO': r.billNo,
          'DESCRIPTIVE OF GOODS': r.desc,
          'QUANTITY': r.qty,
          'UNIT RATE (₹)': Number(r.rate.toFixed(2)),
          'CASH (₹)': r.cash > 0 ? Number(r.cash.toFixed(2)) : '',
          'BANK (₹)': r.bank > 0 ? Number(r.bank.toFixed(2)) : '',
          'TOTAL (₹)': Number(r.total.toFixed(2)),
          'CUSTOMER': r.customer
        });
      });

      grandQty += cQty;
      grandCash += cCash;
      grandBank += cBank;
      grandTotal += cTotal;

      rows.push({
        'DATE': `SUBTOTAL — ${cat.toUpperCase()}`,
        'INVOICE NO': `${list.length} Invoices`,
        'DESCRIPTIVE OF GOODS': '',
        'QUANTITY': Number(cQty.toFixed(2)),
        'UNIT RATE (₹)': '',
        'CASH (₹)': Number(cCash.toFixed(2)),
        'BANK (₹)': Number(cBank.toFixed(2)),
        'TOTAL (₹)': Number(cTotal.toFixed(2)),
        'CUSTOMER': ''
      });

      rows.push({});
    });

    rows.push({
      'DATE': 'GRAND TOTAL MONTHLY SALES',
      'INVOICE NO': 'ALL INVOICES',
      'DESCRIPTIVE OF GOODS': '',
      'QUANTITY': Number(grandQty.toFixed(2)),
      'UNIT RATE (₹)': '',
      'CASH (₹)': Number(grandCash.toFixed(2)),
      'BANK (₹)': Number(grandBank.toFixed(2)),
      'TOTAL (₹)': Number(grandTotal.toFixed(2)),
      'CUSTOMER': ''
    });

    return rows;
  },

  /**
   * exportMonthlyPnLToExcel creates an executive Monthly Income & Expense Excel workbook
   * with prominent product category sales breakdown, detailed item-wise summary,
   * section-grouped sales register, raw material purchases, and operational overhead expenses.
   */
  exportMonthlyPnLToExcel(monthStr, bills, purchases, expenses, settings) {
    if (typeof XLSX === 'undefined') {
      alert('Excel library not loaded.');
      return;
    }

    const s = settings || {};
    let totalPurchasesCash = 0;
    let totalPurchasesBank = 0;
    let totalExpensesCash = 0;
    let totalExpensesBank = 0;

    // 1. Process Product Category Summary
    const catSummary = this.computeCategorySummary(bills);
    const totalSalesCash = catSummary.grandCash;
    const totalSalesBank = catSummary.grandBank;
    const totalSales = catSummary.grandTotal;

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

    // 4. Executive Summary Rows
    const totalPurchases = totalPurchasesCash + totalPurchasesBank;
    const totalExpenses = totalExpensesCash + totalExpensesBank;
    const totalOutflowCash = totalPurchasesCash + totalExpensesCash;
    const totalOutflowBank = totalPurchasesBank + totalExpensesBank;
    const totalOutflow = totalPurchases + totalExpenses;
    const netSurplusCash = totalSalesCash - totalOutflowCash;
    const netSurplusBank = totalSalesBank - totalOutflowBank;
    const netSurplusTotal = totalSales - totalOutflow;

    const summaryRows = [
      { 'FINANCIAL ACCOUNTING HEAD': '=== 1. PRODUCT CATEGORY SALES SUMMARY (TURNOVER) ===', 'QUANTITY / VOLUME': '', 'CASH (₹)': '', 'BANK (₹)': '', 'TOTAL (₹)': '', 'SHARE (%)': '' }
    ];

    catSummary.categories.forEach(c => {
      summaryRows.push({
        'FINANCIAL ACCOUNTING HEAD': c.name,
        'QUANTITY / VOLUME': c.qty > 0 ? `${c.qty.toFixed(2)} ${c.unit}` : '---',
        'CASH (₹)': c.cash,
        'BANK (₹)': c.bank,
        'TOTAL (₹)': c.total,
        'SHARE (%)': `${c.pct}%`
      });
    });

    summaryRows.push({});
    summaryRows.push({ 'FINANCIAL ACCOUNTING HEAD': '=== 2. FINANCIAL RECONCILIATION & NET OPERATING SURPLUS ===', 'QUANTITY / VOLUME': '', 'CASH (₹)': '', 'BANK (₹)': '', 'TOTAL (₹)': '', 'SHARE (%)': '' });
    summaryRows.push({ 'FINANCIAL ACCOUNTING HEAD': '1. GROSS SALES REVENUE (INCOME)', 'QUANTITY / VOLUME': '---', 'CASH (₹)': totalSalesCash, 'BANK (₹)': totalSalesBank, 'TOTAL (₹)': totalSales, 'SHARE (%)': '100.00%' });
    summaryRows.push({ 'FINANCIAL ACCOUNTING HEAD': '2. RAW MATERIAL PURCHASES (COGS)', 'QUANTITY / VOLUME': '---', 'CASH (₹)': totalPurchasesCash, 'BANK (₹)': totalPurchasesBank, 'TOTAL (₹)': totalPurchases, 'SHARE (%)': totalSales > 0 ? `${((totalPurchases / totalSales) * 100).toFixed(2)}%` : '---' });
    summaryRows.push({ 'FINANCIAL ACCOUNTING HEAD': '3. OPERATIONAL & CAPITAL EXPENSES', 'QUANTITY / VOLUME': '---', 'CASH (₹)': totalExpensesCash, 'BANK (₹)': totalExpensesBank, 'TOTAL (₹)': totalExpenses, 'SHARE (%)': totalSales > 0 ? `${((totalExpenses / totalSales) * 100).toFixed(2)}%` : '---' });
    summaryRows.push({ 'FINANCIAL ACCOUNTING HEAD': '---------------------------------------------------', 'QUANTITY / VOLUME': '---', 'CASH (₹)': '------------', 'BANK (₹)': '------------', 'TOTAL (₹)': '------------', 'SHARE (%)': '---' });
    summaryRows.push({ 'FINANCIAL ACCOUNTING HEAD': 'TOTAL GROSS OUTFLOW (2 + 3)', 'QUANTITY / VOLUME': '---', 'CASH (₹)': totalOutflowCash, 'BANK (₹)': totalOutflowBank, 'TOTAL (₹)': totalOutflow, 'SHARE (%)': totalSales > 0 ? `${((totalOutflow / totalSales) * 100).toFixed(2)}%` : '---' });
    summaryRows.push({ 'FINANCIAL ACCOUNTING HEAD': 'NET OPERATIONAL CASH FLOW (SURPLUS)', 'QUANTITY / VOLUME': '---', 'CASH (₹)': netSurplusCash, 'BANK (₹)': netSurplusBank, 'TOTAL (₹)': netSurplusTotal, 'SHARE (%)': totalSales > 0 ? `${((netSurplusTotal / totalSales) * 100).toFixed(2)}%` : '---' });

    // 5. Build Item-Wise Sales Summary Rows
    const itemSummaryRows = this.buildItemWiseSalesRows(bills, totalSales);

    // 6. Build Grouped Sales Register Rows (VFPCL Format)
    const groupedSalesRows = this.buildGroupedSalesRegisterRows(bills);

    const wb = XLSX.utils.book_new();

    // Sheet 1: Executive Summary
    const wsSummary = XLSX.utils.json_to_sheet(summaryRows);
    XLSX.utils.book_append_sheet(wb, wsSummary, 'Executive Summary');

    // Sheet 2: Item-Wise Sales Summary (The requested per-item breakdown)
    const wsItemSummary = XLSX.utils.json_to_sheet(itemSummaryRows);
    XLSX.utils.book_append_sheet(wb, wsItemSummary, 'Item-Wise Sales Summary');

    // Sheet 3: Sales Register (Grouped by Category)
    const wsSales = XLSX.utils.json_to_sheet(groupedSalesRows);
    XLSX.utils.book_append_sheet(wb, wsSales, 'Sales Register');

    // Sheet 4: Purchases
    const wsPurchases = XLSX.utils.json_to_sheet(purchaseRows);
    XLSX.utils.book_append_sheet(wb, wsPurchases, 'Purchases');

    // Sheet 5: Expenses
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

if (typeof window !== 'undefined') {
  window.ExportEngine = ExportEngine;
}
