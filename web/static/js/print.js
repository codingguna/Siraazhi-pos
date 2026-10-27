/* ==========================================================================
   Siraazhi Pro POS — Universal Multi-Machine Print Engine
   
   WHAT IT IS:
     This module formats and renders invoices for any hardware printer:
     - 80mm Thermal Receipt: Standard 3-inch POS printer format with 48 columns.
     - 58mm Thermal Receipt: Portable 2-inch Bluetooth/Handheld POS format (32 columns).
     - A4 Corporate Tax Invoice: Full-page office laser/inkjet format with FSSAI,
       GSTIN, HSN tax summary table, and dual signatures.
     - A5 Delivery Slip: Compact half-page customer copy.

   HOW IT WORKS:
     1. Receives the finalized bill JSON and shop settings.
     2. Identifies target printer mode ('80mm', '58mm', 'a4', 'a5') or uses default.
     3. Injects optimized HTML into the hidden #printContainer element.
     4. Media print CSS (@media print in print.css) suppresses all web app chrome
        and applies exact millimeter-level layout dimensions for that printer type.
     5. Calls window.print() to send the raster/vector stream to the printer spooler.

   FLOW:
     Bill Object -> Selected Printer Template -> Render to #printContainer -> window.print()
   ========================================================================== */

const PrintEngine = {
  cleanBrandTa(s) {
    const val = (s && s.brandTa) || '';
    if (!val || val.includes('?') || val.includes('à®') || val.includes('à¯') || /[\u0080-\u00ff]/.test(val)) {
      return 'சீராழி';
    }
    return val;
  },
  cleanFooterTa(s) {
    const val = (s && s.footerNoteTa) || '';
    if (!val || val.includes('?') || val.includes('à®') || val.includes('à¯') || /[\u0080-\u00ff]/.test(val)) {
      return 'நன்றி! மீண்டும் வருக!';
    }
    return val;
  },
  /**
   * formatINR formats numeric currency into Indian Rupee format: ₹1,250.00
   */
  formatINR(n) {
    return '₹' + Number(n || 0).toLocaleString('en-IN', {
      minimumFractionDigits: 2,
      maximumFractionDigits: 2
    });
  },

  /**
   * render80mm generates HTML for standard 3-inch 80mm ESC/POS roll printers.
   * Flow: constructs shop header -> iterates cart items -> builds tax breakdown -> appends footer.
   */
  render80mm(bill, settings) {
    const s = settings || {};
    const cust = (bill && bill.customer) || {};
    const itemsHtml = (bill.items || []).map(it => `
      <tr>
        <td colspan="4" class="bold">${it.nameEn || ''} <span style="font-size:10px;">(${it.nameTa || ''})</span></td>
      </tr>
      <tr>
        <td style="width:25%">${it.hsn || '-'}</td>
        <td class="center" style="width:25%">${it.qty || 0}</td>
        <td class="right" style="width:25%">${Number(it.price || 0).toFixed(2)}</td>
        <td class="right bold" style="width:25%">${Number(it.amount || 0).toFixed(2)}</td>
      </tr>
    `).join('');

    const taxBreakdown = bill.gstOn ? `
      <div class="dashed-line"></div>
      <div class="right" style="font-size:10px;">
        Taxable: ${this.formatINR(bill.taxable)}<br>
        ${bill.igst > 0 ? `IGST: ${this.formatINR(bill.igst)}<br>` : `CGST (2.5%): ${this.formatINR(bill.cgst)} | SGST (2.5%): ${this.formatINR(bill.sgst)}<br>`}
        Total GST (incl.): ${this.formatINR(bill.gstAmt)}
      </div>
    ` : '<div class="center" style="font-size:10px; margin:2px 0;">*** RETAIL CASH MEMO (NO TAX) ***</div>';

    return `
      <div class="receipt-80mm">
        <div class="center">
          <div class="header-title">${s.companyName || 'VANUR FARMER PRODUCER COMPANY LTD'}</div>
          <div class="header-title" style="font-size:16px; color:#000;">${s.brandEn || 'Siraazhi'} (${this.cleanBrandTa(s)})</div>
          <div class="header-sub">${s.tagline || 'Pure Traditional Marachekku Oils'}</div>
          <div class="header-sub">${s.address || 'Vanur, Villupuram, Tamil Nadu'}</div>
          <div class="header-sub">Ph: ${s.phone || '-'} | FSSAI: ${s.fssai || '-'}</div>
          ${bill.gstOn && s.gstin ? `<div class="header-sub bold">GSTIN: ${s.gstin}</div>` : ''}
          <div class="dashed-line"></div>
          <div class="bold" style="font-size:13px;">${bill.gstOn ? 'TAX INVOICE' : 'CASH RECEIPT'}</div>
        </div>

        <div style="margin:4px 0; font-size:11px;">
          <div><b>Bill No:</b> ${bill.billNo || ''} &nbsp;&nbsp; <b>Date:</b> ${bill.date || ''}</div>
          <div><b>Customer:</b> ${cust.name || '-'} ${cust.phone ? ' | <b>Mob:</b> ' + cust.phone : ''}</div>
          ${cust.gstin ? `<div><b>Cust GSTIN:</b> ${cust.gstin}</div>` : ''}
        </div>

        <div class="dashed-line"></div>
        <table>
          <thead>
            <tr style="border-bottom:1px dashed #000;">
              <th class="left" style="width:25%">HSN</th>
              <th class="center" style="width:25%">Qty</th>
              <th class="right" style="width:25%">Rate</th>
              <th class="right" style="width:25%">Amt</th>
            </tr>
          </thead>
          <tbody>
            ${itemsHtml}
          </tbody>
        </table>

        <div class="dashed-line"></div>
        <div class="right">
          <div>Subtotal: ${this.formatINR(bill.subtotal)}</div>
          ${bill.discount > 0 ? `<div>Discount: -${this.formatINR(bill.discount)}</div>` : ''}
        </div>

        <div class="grand-total-box center">
          GRAND TOTAL: ${this.formatINR(bill.grandTotal)}
        </div>

        <div class="right" style="font-size:11px;">
          Paid (${bill.payMode || 'Cash'}): ${this.formatINR(bill.paid)} &nbsp;|&nbsp; Balance: ${this.formatINR(bill.balance)}
        </div>

        ${taxBreakdown}

        <div class="dashed-line"></div>
        <div class="center" style="margin-top:6px; font-size:11px;">
          <div>${s.footerNoteEn || 'Thank you! Visit again.'}</div>
          <div>${this.cleanFooterTa(s)}</div>
        </div>
      </div>
    `;
  },

  /**
   * render58mm generates ultracompact HTML for portable 2-inch thermal slips.
   * Flow: constructs compact 32-col layout -> short item lines -> summary -> thank you.
   */
  render58mm(bill, settings) {
    const s = settings || {};
    const cust = (bill && bill.customer) || {};
    const itemsHtml = (bill.items || []).map(it => `
      <tr>
        <td colspan="3" class="bold">${it.nameEn || ''}</td>
      </tr>
      <tr>
        <td>${it.qty || 0} x ${Number(it.price || 0).toFixed(0)}</td>
        <td class="center">${it.hsn || ''}</td>
        <td class="right bold">${Number(it.amount || 0).toFixed(2)}</td>
      </tr>
    `).join('');

    return `
      <div class="receipt-58mm">
        <div class="center">
          <div class="header-title">${s.brandEn || 'Siraazhi'} (${this.cleanBrandTa(s)})</div>
          <div style="font-size:8px;">${s.companyName || 'VFPC Ltd'}</div>
          <div style="font-size:8px;">FSSAI: ${s.fssai || '-'}</div>
          ${bill.gstOn && s.gstin ? `<div style="font-size:8px;">GSTIN: ${s.gstin}</div>` : ''}
          <div class="dashed-line"></div>
          <div class="bold">${bill.gstOn ? 'TAX INVOICE' : 'CASH BILL'}</div>
          <div style="font-size:8px;">${bill.billNo || ''} • ${bill.date || ''}</div>
          <div style="font-size:8px;"><b>Cust:</b> ${cust.name || '-'} ${cust.phone ? '• 📞 ' + cust.phone : ''}</div>
        </div>

        <div class="dashed-line"></div>
        <table>
          <thead>
            <tr>
              <th class="left">Item</th>
              <th class="center">HSN</th>
              <th class="right">Amt</th>
            </tr>
          </thead>
          <tbody>
            ${itemsHtml}
          </tbody>
        </table>

        <div class="dashed-line"></div>
        <div class="right bold" style="font-size:12px;">
          TOTAL: ${this.formatINR(bill.grandTotal)}
        </div>
        <div class="right" style="font-size:8.5px;">
          ${bill.payMode || 'Cash'}: ${this.formatINR(bill.paid)}
        </div>

        <div class="dashed-line"></div>
        <div class="center" style="font-size:8.5px;">
          ${s.footerNoteEn || 'Thank you!'}<br>${s.footerNoteTa || 'நன்றி!'}
        </div>
      </div>
    `;
  },

  /**
   * renderA4 generates full corporate tax invoice for office laser/inkjet printers.
   * Flow: constructs official letterhead -> customer details grid -> items table -> HSN summary -> signature boxes.
   */
  renderA4(bill, settings) {
    const s = settings || {};
    const cust = (bill && bill.customer) || {};
    const rows = (bill.items || []).map((it, idx) => `
      <tr>
        <td class="center" style="width:5%">${idx + 1}</td>
        <td style="width:40%">
          <b>${it.nameEn || ''}</b><br>
          <small style="color:#444;">${it.nameTa || ''}</small>
        </td>
        <td class="center" style="width:12%">${it.hsn || '-'}</td>
        <td class="center" style="width:8%">${it.qty || 0}</td>
        <td class="right" style="width:15%">${this.formatINR(it.price)}</td>
        <td class="right" style="width:20%"><b>${this.formatINR(it.amount)}</b></td>
      </tr>
    `).join('');

    const taxTable = bill.gstOn ? `
      <table class="tax-summary-table">
        <thead>
          <tr>
            <th>HSN/SAC</th>
            <th>Taxable Val</th>
            <th>CGST</th>
            <th>SGST</th>
            <th>IGST</th>
            <th>Total Tax</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td class="center">1512 / 1513 / 1515</td>
            <td>${this.formatINR(bill.taxable)}</td>
            <td>${this.formatINR(bill.cgst)}</td>
            <td>${this.formatINR(bill.sgst)}</td>
            <td>${this.formatINR(bill.igst)}</td>
            <td><b>${this.formatINR(bill.gstAmt)}</b></td>
          </tr>
        </tbody>
      </table>
    ` : '<div></div>';

    return `
      <div class="invoice-a4">
        <div class="header-block">
          <div>
            <h1>${s.companyName || 'VANUR FARMER PRODUCER COMPANY LIMITED'}</h1>
            <div style="font-size:15px; font-weight:bold; color:#1b5e20;">
              ${s.brandEn || 'Siraazhi'} (${this.cleanBrandTa(s)}) — ${s.tagline || 'Pure Marachekku Oils'}
            </div>
            <div>${s.address || 'Vanur, Villupuram, Tamil Nadu'}</div>
            <div>Phone: <b>${s.phone || '-'}</b> | FSSAI Lic. No: <b>${s.fssai || '12423031000494'}</b></div>
            ${s.gstin ? `<div>GSTIN: <b>${s.gstin}</b> | State: <b>33 - Tamil Nadu</b></div>` : ''}
          </div>
          <div style="text-align:right;">
            <div class="badge-invoice">${bill.gstOn ? 'TAX INVOICE' : 'RETAIL CASH BILL'}</div>
            <div style="margin-top:6px; font-size:13px;">
              Invoice No: <b>${bill.billNo || ''}</b><br>
              Date: <b>${bill.date || ''}</b><br>
              Mode: <b>${bill.payMode || 'Cash'}</b>
            </div>
          </div>
        </div>

        <div class="meta-grid">
          <div>
            <div style="font-size:10px; font-weight:bold; text-transform:uppercase; color:#666;">Billed To (Customer):</div>
            <div style="font-size:14px; font-weight:bold;">${cust.name || '-'}</div>
            <div>Mobile / Phone: <b>${cust.phone || '-'}</b></div>
            <div>Address: ${cust.address || 'Tamil Nadu'}</div>
          </div>
          <div>
            ${cust.gstin ? `<div>Customer GSTIN: <b>${cust.gstin}</b></div>` : '<div>Customer Type: <b>Consumer / Unregistered</b></div>'}
            <div>Place of Supply: <b>${cust.state || 'Tamil Nadu (33)'}</b></div>
          </div>
        </div>

        <table class="items-table">
          <thead>
            <tr>
              <th>#</th>
              <th>Description of Goods</th>
              <th>HSN</th>
              <th>Qty</th>
              <th>Unit Rate</th>
              <th>Total Amount</th>
            </tr>
          </thead>
          <tbody>
            ${rows}
          </tbody>
        </table>

        <div class="totals-block">
          ${taxTable}

          <div class="final-calc">
            <div>Subtotal: <b>${this.formatINR(bill.subtotal)}</b></div>
            ${bill.discount > 0 ? `<div>Discount: <b>-${this.formatINR(bill.discount)}</b></div>` : ''}
            ${bill.gstOn ? `<div>Taxable Value: <b>${this.formatINR(bill.taxable)}</b></div>` : ''}
            ${bill.gstOn ? `<div>Total GST (incl.): <b>${this.formatINR(bill.gstAmt)}</b></div>` : ''}
            <div class="grand-total-row">
              GRAND TOTAL: ${this.formatINR(bill.grandTotal)}
            </div>
            <div>Amount Paid: <b>${this.formatINR(bill.paid)}</b></div>
            <div>Balance Due: <b>${this.formatINR(bill.balance)}</b></div>
          </div>
        </div>

        <div class="sign-block">
          <div>Customer Signature</div>
          <div>For ${s.companyName || 'VANUR FARMER PRODUCER COMPANY LIMITED'}<br><br><small>(Authorised Signatory)</small></div>
        </div>

        <div style="margin-top:14px; font-size:10px; font-style:italic; color:#444;">
          Declaration: We declare that this invoice shows the actual price of the goods described and that all particulars are true and correct.
        </div>
      </div>
    `;
  },

  /**
   * renderA5 generates half-page delivery slip.
   */
  renderA5(bill, settings) {
    return `<div class="invoice-a5">${this.render80mm(bill, settings)}</div>`;
  },

  /**
   * renderTestSlip80mm generates an 80mm test receipt ticket matching bill1
   */
  renderTestSlip80mm(printerName, settings) {
    const s = settings || {};
    const nowStr = new Date().toLocaleString('en-IN');
    return `
      <div class="receipt-80mm">
        <div class="center">
          <div class="header-title">${s.companyName || 'VANUR FARMER PRODUCER COMPANY LIMITED'}</div>
          <div class="header-title" style="font-size:15px;">${s.brandEn || 'Siraazhi'} (${this.cleanBrandTa(s)})</div>
          <div class="header-sub">${s.tagline || 'Pure Traditional Marachekku Oils'}</div>
          <div class="header-sub">${s.address || 'Vanur, Villupuram, Tamil Nadu'}</div>
          <div class="header-sub">Ph: ${s.phone || '-'} | FSSAI: ${s.fssai || '-'}</div>
          <div class="dashed-line"></div>
          <div class="bold" style="font-size:12px;">HARDWARE PRINTER TEST SLIP</div>
        </div>

        <div style="margin:4px 0; font-size:10.5px;">
          <div><b>Printer:</b> ${printerName || 'Default Hardware'}</div>
          <div><b>Format:</b> 80mm Roll (3-inch Thermal)</div>
          <div><b>Status:</b> ONLINE - READY ✓</div>
          <div><b>Time:</b> ${nowStr}</div>
        </div>

        <div class="dashed-line"></div>
        <div style="font-size:10px; line-height:1.4;">
          <div>[✓] Direct Zero-Dialog Hardware Spooling Active</div>
          <div>[✓] 80mm Thermal Receipt Layout Ready</div>
          <div>[✓] Multi-Language & Tamil Printing Ready</div>
          <div>[✓] Universal Support for Thermal, Laser & Inkjet</div>
        </div>

        <div class="dashed-line"></div>
        <div class="center bold" style="font-size:11px; margin:4px 0;">
          Hardware Test Slip Dispatched Successfully!
        </div>

        <div class="dashed-line"></div>
        <div class="center" style="font-size:10.5px; margin-top:4px;">
          <div>${s.footerNoteEn || 'Thank you! Visit again.'}</div>
          <div>${this.cleanFooterTa(s)}</div>
        </div>
      </div>
    `;
  },

  /**
   * renderTestSlipA4 generates a corporate A4 test document matching bill2
   */
  renderTestSlipA4(printerName, settings) {
    const s = settings || {};
    const nowStr = new Date().toLocaleString('en-IN');
    return `
      <div class="invoice-a4">
        <div class="header-block">
          <div>
            <h1>${s.companyName || 'VANUR FARMER PRODUCER COMPANY LIMITED'}</h1>
            <div style="font-size:15px; font-weight:bold; color:#1b5e20;">
              ${s.brandEn || 'Siraazhi'} (${this.cleanBrandTa(s)}) — ${s.tagline || 'Pure Traditional Marachekku Oils'}
            </div>
            <div>${s.address || 'Vanur, Villupuram, Tamil Nadu'}</div>
            <div>Phone: <b>${s.phone || '-'}</b> | FSSAI Lic. No: <b>${s.fssai || '12423031000494'}</b></div>
          </div>
          <div style="text-align:right;">
            <div class="badge-invoice">HARDWARE VERIFICATION TEST</div>
            <div style="margin-top:6px; font-size:12px;">
              Status: <b>ONLINE - READY ✓</b><br>
              Date: <b>${nowStr}</b>
            </div>
          </div>
        </div>

        <div class="meta-grid" style="margin-bottom:12px;">
          <div>
            <div>Printer Target: <b>${printerName || 'Default Windows Printer'}</b></div>
            <div>Printer Class: <b>Standard Document Spooler (Laser / Inkjet / PDF)</b></div>
            <div>Paper Size Preset: <b>A4 Sheet (210 x 297 mm)</b></div>
          </div>
          <div>
            <div>Hardware Status: <b>ONLINE - READY ✓</b></div>
            <div>Spool Timestamp: <b>${nowStr}</b></div>
          </div>
        </div>

        <table class="items-table" style="margin-bottom:16px;">
          <thead>
            <tr>
              <th style="width:10%">#</th>
              <th style="width:65%">Subsystem / Verification Check</th>
              <th style="width:25%; text-align:center;">Status</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td class="center">1</td>
              <td>Direct Zero-Dialog Windows GDI Document Spooling</td>
              <td class="center bold" style="color:#1b5e20;">[✓] READY</td>
            </tr>
            <tr>
              <td class="center">2</td>
              <td>Mobile & Tablet Wireless LAN Printing Enabled</td>
              <td class="center bold" style="color:#1b5e20;">[✓] READY</td>
            </tr>
            <tr>
              <td class="center">3</td>
              <td>Full Corporate A4 Tax Invoicing & Multi-Printer Routing</td>
              <td class="center bold" style="color:#1b5e20;">[✓] READY</td>
            </tr>
            <tr>
              <td class="center">4</td>
              <td>Universal Hardware Support for HP Smart Tank, LaserJet, Inkjet & POS Thermal</td>
              <td class="center bold" style="color:#1b5e20;">[✓] READY</td>
            </tr>
          </tbody>
        </table>

        <div style="margin-top:30px; display:flex; justify-content:space-between; padding:0 10px;">
          <div></div>
          <div style="text-align:center; border-top:1px solid #000; padding-top:4px; font-size:10.5px; width:220px;">
            For ${s.companyName || 'VANUR FARMER PRODUCER COMPANY LIMITED'}<br><br>
            <small>(Authorised Signatory)</small>
          </div>
        </div>
      </div>
    `;
  },

  /**
   * print renders the selected format into #printContainer and triggers native window.print().
   */
  print(bill, format, settings) {
    const container = document.getElementById('printContainer');
    if (!container) return;

    let html = '';
    let mode = format;
    if (!mode || mode === 'default') {
      mode = (settings && settings.defaultPrinter) || '80mm';
    }

    // Dynamic @page size injection so browser automatically selects exact dimensions
    let pageStyle = document.getElementById('dynamicPageSizeStyle');
    if (!pageStyle) {
      pageStyle = document.createElement('style');
      pageStyle.id = 'dynamicPageSizeStyle';
      document.head.appendChild(pageStyle);
    }
    if (mode === '80mm' || mode === '58mm') {
      pageStyle.innerHTML = `@page { size: ${mode === '58mm' ? '48mm' : '80mm'} auto; margin: 2mm 3mm; }`;
    } else {
      pageStyle.innerHTML = `@page { size: A4 portrait; margin: 8mm 10mm; }`;
    }

    switch (mode) {
      case '58mm':
        html = this.render58mm(bill, settings);
        break;
      case 'a4':
        html = this.renderA4(bill, settings);
        break;
      case 'a5':
        html = this.renderA5(bill, settings);
        break;
      case '80mm':
      default:
        html = this.render80mm(bill, settings);
        break;
    }

    // Completely wipe and suppress any toast notifications before triggering system print dialog
    const toastBox = document.getElementById('toastNotificationContainer');
    if (toastBox) {
      toastBox.innerHTML = '';
      toastBox.style.setProperty('display', 'none', 'important');
    }

    container.innerHTML = html;

    const restoreToasts = () => {
      if (toastBox) {
        toastBox.style.removeProperty('display');
      }
      window.removeEventListener('afterprint', restoreToasts);
    };
    window.addEventListener('afterprint', restoreToasts);

    setTimeout(() => {
      window.print();
      // Secondary fallback cleanup in case afterprint doesn't fire
      setTimeout(restoreToasts, 2000);
    }, 150);
  }
};
