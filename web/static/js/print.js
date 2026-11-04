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

const SIRAAZHI_LOGO_BASE64 = '/assets/siraazhi-logo.jpg';

const VFPCL_LOGO_BASE64 = '/assets/vfpcl-logo.jpg';
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
   * numberToWordsINR converts a numeric amount to Indian Rupee word representation.
   * e.g. 10300 -> "INR Ten Thousand Three Hundred only"
   */
  numberToWordsINR(amount) {
    const num = Math.round(Number(amount || 0));
    if (num === 0) return 'INR Zero only';

    const a = [
      '', 'One', 'Two', 'Three', 'Four', 'Five', 'Six', 'Seven', 'Eight', 'Nine', 'Ten',
      'Eleven', 'Twelve', 'Thirteen', 'Fourteen', 'Fifteen', 'Sixteen', 'Seventeen', 'Eighteen', 'Nineteen'
    ];
    const b = ['', '', 'Twenty', 'Thirty', 'Forty', 'Fifty', 'Sixty', 'Seventy', 'Eighty', 'Ninety'];

    function convertUnderThousand(n) {
      let str = '';
      if (n >= 100) {
        str += a[Math.floor(n / 100)] + ' Hundred ';
        n %= 100;
      }
      if (n >= 20) {
        str += b[Math.floor(n / 10)] + (n % 10 > 0 ? ' ' + a[n % 10] : '') + ' ';
      } else if (n > 0) {
        str += a[n] + ' ';
      }
      return str;
    }

    let words = '';
    let n = num;

    const crore = Math.floor(n / 10000000);
    n %= 10000000;
    const lakh = Math.floor(n / 100000);
    n %= 100000;
    const thousand = Math.floor(n / 1000);
    n %= 1000;

    if (crore > 0) words += convertUnderThousand(crore) + 'Crore ';
    if (lakh > 0) words += convertUnderThousand(lakh) + 'Lakh ';
    if (thousand > 0) words += convertUnderThousand(thousand) + 'Thousand ';
    if (n > 0) words += convertUnderThousand(n);

    return 'INR ' + words.trim().replace(/\s+/g, ' ') + ' only';
  },

  formatPayMode(bill) {
    if (!bill) return 'Cash';
    if (bill.payMode === 'Split' || (bill.cashPaid > 0 && bill.upiPaid > 0)) {
      return `Split (Cash: ${this.formatINR(bill.cashPaid)}, UPI: ${this.formatINR(bill.upiPaid)})`;
    }
    return bill.payMode || 'Cash';
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
          Paid: <b>${this.formatINR(bill.paid)}</b> (${this.formatPayMode(bill)}) &nbsp;|&nbsp; Balance: ${this.formatINR(bill.balance)}
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
          ${this.formatPayMode(bill)}: ${this.formatINR(bill.paid)}
        </div>

        <div class="dashed-line"></div>
        <div class="center" style="font-size:8.5px;">
          ${s.footerNoteEn || 'Thank you!'}<br>${s.footerNoteTa || 'நன்றி!'}
        </div>
      </div>
    `;
  },

  /**
   * renderA4 generates corporate tax invoice for office laser/inkjet printers.
   * If printCopies is 2, it prints BOTH copies on the SAME A4 paper (top & bottom half with middle cut line).
   * If printCopies is 1, it prints a single full-page invoice (no change).
   */
  renderA4(bill, settings) {
    const s = settings || {};
    const copies = parseInt(s.printCopies, 10) || 1;

    if (copies === 2) {
      const topHalf = this.renderA4Single(bill, s, 'ORIGINAL FOR RECIPIENT', true);
      const bottomHalf = this.renderA4Single(bill, s, 'DUPLICATE FOR SUPPLIER', true);
      const cutLine = `
        <div class="a4-cutting-line">
          <span>✂ - - - - - - - - - - - - - - - - - - - - CUT HERE - - - - - - - - - - - - - - - - - - - - ✂</span>
        </div>
      `;
      return `
        <div class="invoice-a4-two-up">
          ${topHalf}
          ${cutLine}
          ${bottomHalf}
        </div>
      `;
    }

    return this.renderA4Single(bill, s, '', false);
  },

  /**
   * renderA4Single renders a single A4 tax invoice block.
   * When isHalf is true, it renders a compact half-page version suited for 2-in-1 same paper printing.
   */
  renderA4Single(bill, settings, copyLabel, isHalf) {
    const s = settings || {};
    const cust = (bill && bill.customer) || {};
    const deliveryType = bill.deliveryType || 'Office';
    const payMode = bill.payMode || 'Cash';
    const placeOfSupply = (cust && cust.state) ? `${cust.state}` : 'Office (Tamil Nadu - 33)';

    // Group items for HSN tax breakdown (reverse-calculated from inclusive net amount)
    const hsnMap = {};
    let totalTaxableAll = 0;
    let totalCgstAll = 0;
    let totalSgstAll = 0;
    let totalTaxAll = 0;

    const rows = (bill.items || []).map((it, idx) => {
      const qty = Number(it.qty) || 0;
      const netPrice = Number(it.price) || 0;
      const lineAmt = Number(it.amount) || (qty * netPrice);
      const mrp = Number(it.mrp) > 0 ? Number(it.mrp) : netPrice;
      const taxRate = Number(it.gstPct !== undefined ? it.gstPct : (s.gstDefaultPct || 5));

      // Calculate discount % per unit (MRP - NetPrice)
      let discPct = 0;
      if (mrp > netPrice && mrp > 0) {
        discPct = ((mrp - netPrice) / mrp) * 100;
      }
      const discPerUnit = mrp > netPrice ? (mrp - netPrice) : 0;

      // Reverse tax calculation: taxable = lineAmt / (1 + taxRate/100)
      if (bill.gstOn && taxRate > 0) {
        const taxableLine = lineAmt / (1 + (taxRate / 100));
        const taxLine = lineAmt - taxableLine;
        const halfTax = taxLine / 2;

        const hsnKey = `${it.hsn || '1512'}_${taxRate}`;
        if (!hsnMap[hsnKey]) {
          hsnMap[hsnKey] = {
            hsn: it.hsn || '1512',
            taxRate: taxRate,
            taxableAmt: 0,
            sgstAmt: 0,
            cgstAmt: 0,
            totalTax: 0
          };
        }
        hsnMap[hsnKey].taxableAmt += taxableLine;
        hsnMap[hsnKey].sgstAmt += halfTax;
        hsnMap[hsnKey].cgstAmt += halfTax;
        hsnMap[hsnKey].totalTax += taxLine;

        totalTaxableAll += taxableLine;
        totalCgstAll += halfTax;
        totalSgstAll += halfTax;
        totalTaxAll += taxLine;
      }

      return `
        <tr>
          <td class="center" style="width:4%;">${idx + 1}</td>
          <td style="width:36%;">
            <b>${it.nameEn || ''}</b>
            ${it.nameTa ? `<br><small style="color:#555;">${it.nameTa}</small>` : ''}
            <div style="font-size:8.5px; color:#555;">HSN: ${it.hsn || '-'}</div>
          </td>
          <td class="center" style="width:8%; font-weight:bold;">${qty}</td>
          <td class="center" style="width:8%;">${taxRate.toFixed(1)}%</td>
          <td class="right" style="width:11%;">${this.formatINR(mrp)}</td>
          <td class="center" style="width:10%;">
            ${discPct > 0 ? `${discPct.toFixed(1)}%<br><small style="color:#666;">(-₹${discPerUnit.toFixed(1)})</small>` : '0%'}
          </td>
          <td class="right" style="width:11%;">${this.formatINR(netPrice)}</td>
          <td class="right bold" style="width:12%;">${this.formatINR(lineAmt)}</td>
        </tr>
      `;
    }).join('');

    let blankRows = '';
    // Only add blank padding rows for GST tax invoices or when explicitly configured,
    // to keep non-GST bills compact and clean without unnecessary empty rows.
    if (bill.gstOn) {
      const minRows = isHalf ? 3 : 5;
      const count = (bill.items || []).length;
      for (let i = count; i < minRows; i++) {
        blankRows += `
          <tr class="blank-row">
            <td class="center">&nbsp;</td>
            <td>&nbsp;</td>
            <td class="center">&nbsp;</td>
            <td class="center">&nbsp;</td>
            <td class="right">&nbsp;</td>
            <td class="center">&nbsp;</td>
            <td class="right">&nbsp;</td>
            <td class="right">&nbsp;</td>
          </tr>
        `;
      }
    }

    // Build HSN Tax Breakdown Table rows
    const hsnKeys = Object.keys(hsnMap);
    let hsnRowsHtml = '';
    if (bill.gstOn && hsnKeys.length > 0) {
      hsnRowsHtml = hsnKeys.map(k => {
        const item = hsnMap[k];
        return `
          <tr>
            <td class="center">${item.hsn}</td>
            <td class="center">${item.taxRate.toFixed(0)}%</td>
            <td class="right">${this.formatINR(item.taxableAmt)}</td>
            <td class="right">${this.formatINR(item.sgstAmt)}</td>
            <td class="right">${this.formatINR(item.cgstAmt)}</td>
            <td class="right bold">${this.formatINR(item.totalTax)}</td>
          </tr>
        `;
      }).join('');
    } else {
      hsnRowsHtml = `
        <tr>
          <td class="center">1512 / 1513</td>
          <td class="center">0%</td>
          <td class="right">${this.formatINR(bill.grandTotal)}</td>
          <td class="right">₹0.00</td>
          <td class="right">₹0.00</td>
          <td class="right bold">₹0.00</td>
        </tr>
      `;
    }

    const copyBadge = copyLabel ? `<div class="copy-tag">${copyLabel}</div>` : '';
    const totalWords = this.numberToWordsINR(bill.grandTotal);
    const invoiceTitle = bill.gstOn ? 'Tax Invoice' : 'Invoice';

    return `
      <div class="invoice-a4 ${isHalf ? 'invoice-a4-half' : ''}">
        <div class="half-top-block">
          ${copyBadge}
          
          <!-- Top Center Document Title -->
          <div class="a4-doc-title-bar">
            <span class="a4-doc-title">${invoiceTitle}</span>
          </div>

          <!-- Header Section: Left/Center Brand, Right Shop Contact -->
          <div class="header-block">
            <div class="header-left">
              <h1 class="company-name">${s.companyName || 'VANUR FARMER PRODUCER COMPANY LIMITED'}</h1>
              <div class="brand-line">
                <span class="brand-en">${s.brandEn || 'Siraazhi'}</span>
                <span class="brand-ta">(${this.cleanBrandTa(s)})</span>
                <span class="tagline">— ${s.tagline || 'Pure Traditional Marachekku Oils'}</span>
              </div>
              <div class="fssai-line">FSSAI Lic. No: <b>${s.fssai || '12423031000494'}</b></div>
            </div>
            <div class="header-right">
              <div>${s.address || 'Vanur, Villupuram, Tamil Nadu'}</div>
              <div><b>GSTIN:</b> ${s.gstin || '33AAJCV1242R1ZY'}</div>
              <div><b>Phone:</b> ${s.phone || '9751680093'}</div>
              <div><b>Email:</b> ${s.email || 'vanurfpc@gmail.com'}</div>
            </div>
          </div>

          <!-- Billed To (Left), Dual Logos (Center) & Invoice Metadata (Right) -->
          <div class="meta-grid">
            <div class="meta-billed-to">
              <div class="meta-label">Billed To :</div>
              <div class="customer-name">${cust.name || 'Walk-in Customer'}</div>
              <div><b>Mobile:</b> ${cust.phone || '-'}</div>
              <div><b>Address:</b> ${cust.address || 'Office'}</div>
              ${cust.gstin ? `<div><b>GSTIN:</b> ${cust.gstin}</div>` : ''}
            </div>
            <div class="meta-logo-center">
              <img src="${VFPCL_LOGO_BASE64}" alt="VFPCL Logo" class="a4-center-logo vfpcl-logo" title="Vanur Farmer Producer Company Limited">
              <img src="${SIRAAZHI_LOGO_BASE64}" alt="Siraazhi Logo" class="a4-center-logo siraazhi-logo" title="Siraazhi Brand">
            </div>
            <div class="meta-invoice-info">
              <div><b>Bill No:</b> ${bill.billNo || ''}</div>
              <div><b>Date:</b> ${bill.date || ''}</div>
              <div><b>Payment Type:</b> ${payMode}</div>
              <div><b>Delivery Type:</b> ${deliveryType}</div>
              <div><b>Place of Supply:</b> ${placeOfSupply}</div>
            </div>
          </div>

          <!-- Product Line Items Table -->
          <table class="items-table">
            <thead>
              <tr>
                <th style="width:4%;">S.No</th>
                <th style="width:36%;">Description of Goods</th>
                <th style="width:8%;">Qty</th>
                <th style="width:8%;">Tax Rate</th>
                <th style="width:11%;">MRP Price</th>
                <th style="width:10%;">Disc. %</th>
                <th style="width:11%;">Net Price</th>
                <th style="width:12%;">Amount</th>
              </tr>
            </thead>
            <tbody>
              ${rows}
              ${blankRows}
            </tbody>
          </table>
        </div>

        <div class="half-bottom-block">
          <!-- Tax Calculation Section & Overall Totals -->
          ${bill.gstOn ? `
            <div class="totals-block">
              <!-- Left HSN Summary Table -->
              <div class="hsn-summary-wrapper">
                <table class="tax-summary-table">
                  <thead>
                    <tr>
                      <th>HSN Code</th>
                      <th>Tax Rate</th>
                      <th>Taxable Amt</th>
                      <th>SGST Amt</th>
                      <th>CGST Amt</th>
                      <th>Total Tax</th>
                    </tr>
                  </thead>
                  <tbody>
                    ${hsnRowsHtml}
                  </tbody>
                </table>
              </div>

              <!-- Right Calculation Summary -->
              <div class="final-calc">
                <div class="calc-row"><span>Sub Total:</span> <b>${this.formatINR(bill.subtotal)}</b></div>
                ${bill.discount > 0 ? `<div class="calc-row"><span>Discount:</span> <b>-${this.formatINR(bill.discount)}</b></div>` : ''}
                <div class="calc-row"><span>Taxable Amount:</span> <b>${this.formatINR(totalTaxableAll || bill.taxable)}</b></div>
                <div class="calc-row"><span>Add : SGST:</span> <b>${this.formatINR(totalSgstAll || bill.sgst)}</b></div>
                <div class="calc-row"><span>Add : CGST:</span> <b>${this.formatINR(totalCgstAll || bill.cgst)}</b></div>
              </div>
            </div>
          ` : (bill.discount > 0 ? `
            <div class="totals-block non-gst-calc">
              <div class="final-calc" style="width:100%; border:1px solid #000; border-bottom:none; padding:4px 8px;">
                <div class="calc-row"><span>Sub Total:</span> <b>${this.formatINR(bill.subtotal)}</b></div>
                <div class="calc-row"><span>Discount:</span> <b>-${this.formatINR(bill.discount)}</b></div>
              </div>
            </div>
          ` : '')}

          <!-- Worded Amount (Left) & Total Number (Right) on the Same Row -->
          <div class="grand-total-banner">
            <div class="grand-total-words">
              <b>${totalWords}</b>
            </div>
            <div class="grand-total-number">
              <span class="total-lbl">TOTAL:</span>
              <span class="total-val">${this.formatINR(bill.grandTotal)}</span>
            </div>
          </div>

          <!-- Dual Authority & Customer Signatures -->
          <div class="sign-block">
            <div class="cust-sign">
              <div class="sign-line"></div>
              <span>Receiver's / Customer Signature</span>
            </div>
            <div class="auth-sign">
              <div class="sign-company">For ${s.companyName || 'VANUR FARMER PRODUCER COMPANY LIMITED'}</div>
              <div class="sign-line" style="margin-top:22px;"></div>
              <span>Authorised Signatory</span>
            </div>
          </div>

          <div class="footer-terms">
            (E&OE) We declare that this invoice shows the actual price of goods described and all particulars are true and correct.
          </div>
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
      const copies = (settings && parseInt(settings.printCopies, 10)) || 1;
      if (mode === 'a4' && copies === 2) {
        pageStyle.innerHTML = `@page { size: A4 portrait; margin: 4mm 6mm; }`;
      } else {
        pageStyle.innerHTML = `@page { size: A4 portrait; margin: 8mm 10mm; }`;
      }
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
  },

  /**
   * printCustomerLedger prints an A4 Statement of Account identical to standard commercial party ledgers.
   */
  printCustomerLedger(ledger, settings) {
    if (!ledger) {
      alert('No ledger statement data to print.');
      return;
    }

    const container = document.getElementById('printContainer');
    if (!container) return;

    const s = settings || {};
    const cust = ledger.customer || {};
    const company = s.companyName || 'VANUR FARMER PRODUCER COMPANY LIMITED';
    const compAddr = (s.address || 'Villupuram, Tamil Nadu') + (s.gstin ? ` • GSTIN: <b>${s.gstin}</b>` : '') + (s.phone ? ` • Ph: <b>${s.phone}</b>` : '');
    const periodStr = (ledger.fromDate || 'Start') + ' to ' + (ledger.toDate || 'Present');

    // Page styling for clean A4 print
    let pageStyle = document.getElementById('dynamicPrintPageStyle');
    if (!pageStyle) {
      pageStyle = document.createElement('style');
      pageStyle.id = 'dynamicPrintPageStyle';
      document.head.appendChild(pageStyle);
    }
    pageStyle.innerHTML = `@page { size: A4 portrait; margin: 8mm 10mm; }`;

    let rowsHtml = '';

    // Opening Balance Row
    if (ledger.openingBalance) {
      rowsHtml += `
        <tr style="background:#f8fafc; font-style:italic;">
          <td style="padding:4px 6px;">${ledger.fromDate || ''}</td>
          <td style="padding:4px 6px;">—</td>
          <td style="padding:4px 6px;"><b>Opening Balance (கையிருப்பு இருப்பு)</b></td>
          <td style="text-align:right; padding:4px 6px;">${ledger.openingBalance > 0 ? this.formatINR(ledger.openingBalance) : ''}</td>
          <td style="text-align:right; padding:4px 6px;">${ledger.openingBalance < 0 ? this.formatINR(Math.abs(ledger.openingBalance)) : ''}</td>
          <td style="text-align:right; padding:4px 6px; font-weight:bold;">${this.formatINR(ledger.openingBalance)}</td>
        </tr>
      `;
    }

    // Ledger Transaction Entries
    (ledger.entries || []).forEach(e => {
      rowsHtml += `
        <tr style="border-bottom:1px solid #e2e8f0;">
          <td style="padding:4px 6px; white-space:nowrap;">${e.date}</td>
          <td style="padding:4px 6px; font-weight:600;">${e.vchNo || '—'}</td>
          <td style="padding:4px 6px;">${e.particulars || '—'}</td>
          <td style="text-align:right; padding:4px 6px; font-weight:${e.debit > 0 ? '600' : 'normal'};">${e.debit > 0 ? this.formatINR(e.debit) : ''}</td>
          <td style="text-align:right; padding:4px 6px; font-weight:${e.credit > 0 ? '600' : 'normal'};">${e.credit > 0 ? this.formatINR(e.credit) : ''}</td>
          <td style="text-align:right; padding:4px 6px; font-weight:bold;">${this.formatINR(e.balance)}</td>
        </tr>
      `;
    });

    const closingDebit = (ledger.totalDebit || 0) + (ledger.openingBalance > 0 ? ledger.openingBalance : 0);
    const closingCredit = (ledger.totalCredit || 0) + (ledger.openingBalance < 0 ? Math.abs(ledger.openingBalance) : 0);
    const balType = ledger.balanceType || (ledger.closingBalance >= 0 ? 'Debit Balance' : 'Credit Balance');

    const html = `
      <div style="font-family:'Segoe UI', Arial, sans-serif; color:#0f172a; max-width:100%; padding:4px 8px; box-sizing:border-box;">
        
        <!-- Header -->
        <div style="text-align:center; margin-bottom:12px; border-bottom:1px solid #000; padding-bottom:8px;">
          <h1 style="margin:0; font-size:17px; font-weight:800; text-transform:uppercase; letter-spacing:0.5px;">${company}</h1>
          <div style="font-size:11px; color:#334155; margin-top:2px;">${compAddr}</div>
          <div style="font-size:14px; font-weight:800; letter-spacing:1px; margin-top:8px; text-decoration:underline; text-transform:uppercase;">CUSTOMER LEDGER</div>
          <div style="font-size:11px; font-weight:600; color:#475569; margin-top:2px;">(From ${periodStr})</div>
        </div>

        <!-- Account Info -->
        <div style="margin-bottom:10px; font-size:11.5px; border-bottom:1px solid #000; padding-bottom:6px; line-height:1.5;">
          <div><b>Account:</b> <span style="font-weight:700; font-size:12.5px; text-transform:uppercase;">${cust.name || 'Walk-in'}</span></div>
          <div>${cust.address || ''}${cust.state ? ', ' + cust.state : ''}${cust.gstin ? ' • <b>GSTIN:</b> ' + cust.gstin : ''}${cust.phone ? ' • <b>Mobile:</b> ' + cust.phone : ''}</div>
        </div>

        <!-- Ledger Table -->
        <table style="width:100%; border-collapse:collapse; font-size:11px;">
          <thead>
            <tr style="border-top:1.5px solid #000; border-bottom:1.5px solid #000; background:#f1f5f9;">
              <th style="text-align:left; padding:5px 6px; width:85px;">Date</th>
              <th style="text-align:left; padding:5px 6px; width:130px;">Vch No</th>
              <th style="text-align:left; padding:5px 6px;">Particulars</th>
              <th style="text-align:right; padding:5px 6px; width:100px;">Debit</th>
              <th style="text-align:right; padding:5px 6px; width:100px;">Credit</th>
              <th style="text-align:right; padding:5px 6px; width:100px;">Balance</th>
            </tr>
          </thead>
          <tbody>
            ${rowsHtml || '<tr><td colspan="6" style="text-align:center; padding:16px;">No transactions found in this period.</td></tr>'}
          </tbody>
          <tfoot>
            <tr style="border-top:1.5px solid #000; border-bottom:1.5px solid #000; font-weight:bold;">
              <td colspan="3" style="text-align:left; padding:6px 6px;">Total</td>
              <td style="text-align:right; padding:6px 6px;">${this.formatINR(ledger.totalDebit)}</td>
              <td style="text-align:right; padding:6px 6px;">${this.formatINR(ledger.totalCredit)}</td>
              <td></td>
            </tr>
            <tr style="font-weight:bold;">
              <td colspan="3" style="text-align:left; padding:5px 6px;">Closing (Opening + Total)</td>
              <td style="text-align:right; padding:5px 6px;">${this.formatINR(closingDebit)}</td>
              <td style="text-align:right; padding:5px 6px;">${this.formatINR(closingCredit)}</td>
              <td></td>
            </tr>
            <tr style="border-bottom:1.5px solid #000; font-weight:800; font-size:12px; background:#f8fafc;">
              <td colspan="3" style="text-align:left; padding:6px 6px;">${balType}</td>
              <td colspan="3" style="text-align:right; padding:6px 6px; font-size:13px; color:#0f172a;">${this.formatINR(Math.abs(ledger.closingBalance))}</td>
            </tr>
          </tfoot>
        </table>

        <!-- Signatory Footer -->
        <div style="display:flex; justify-content:space-between; margin-top:35px; font-size:11px;">
          <div style="padding-top:20px;">Prepared & Checked By: ___________________</div>
          <div style="text-align:right;">
            <div><b>For ${company}</b></div>
            <div style="margin-top:35px;">Authorized Signatory</div>
          </div>
        </div>

      </div>
    `;

    // Suppress toasts before printing
    const toastBox = document.getElementById('toastNotificationContainer');
    if (toastBox) {
      toastBox.innerHTML = '';
      toastBox.style.setProperty('display', 'none', 'important');
    }

    container.innerHTML = html;

    const restoreToasts = () => {
      if (toastBox) toastBox.style.removeProperty('display');
      window.removeEventListener('afterprint', restoreToasts);
    };
    window.addEventListener('afterprint', restoreToasts);

    setTimeout(() => {
      window.print();
      setTimeout(restoreToasts, 2000);
    }, 150);
  }
};
