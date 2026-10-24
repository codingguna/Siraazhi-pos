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
          <div class="header-title" style="font-size:16px; color:#000;">${s.brandEn || 'Siraazhi'} (${s.brandTa || 'சீராழி'})</div>
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
          <div>${s.footerNoteTa || 'நன்றி! மீண்டும் வருக!'}</div>
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
          <div class="header-title">${s.brandEn || 'Siraazhi'} (${s.brandTa || 'சீராழி'})</div>
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
              ${s.brandEn || 'Siraazhi'} (${s.brandTa || 'சீராழி'}) — ${s.tagline || 'Pure Marachekku Oils'}
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
          <div>For ${s.companyName || 'VANUR FPC LTD'}<br><br><small>(Authorised Signatory)</small></div>
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
