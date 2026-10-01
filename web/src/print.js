// 打印工具：新开窗口输出打印版并唤起打印
export function printHTML(title, bodyHtml) {
  const w = window.open('', '_blank', 'width=900,height=700')
  if (!w) {
    alert('浏览器拦截了打印窗口，请允许弹出窗口后重试')
    return
  }
  w.document.write(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>${title}</title>
<style>
  body { font-family: -apple-system, "PingFang SC", "Microsoft YaHei", serif; margin: 32px; color: #111; }
  h1 { font-size: 18px; text-align: center; margin: 0 0 4px; }
  .meta { text-align: center; color: #555; font-size: 12px; margin-bottom: 20px; }
  table { width: 100%; border-collapse: collapse; font-size: 12px; margin-bottom: 16px; }
  th, td { border: 1px solid #444; padding: 6px 8px; }
  th { background: #f0f0f0; }
  .num { text-align: right; font-variant-numeric: tabular-nums; }
  .v-head { display: flex; justify-content: space-between; font-size: 13px; margin-bottom: 12px; }
  .v-grid { width: 100%; border-collapse: collapse; font-size: 13px; margin-bottom: 16px; }
  .v-grid td { border: 1px solid #444; padding: 8px; }
  .sign { display: flex; justify-content: space-between; margin-top: 48px; font-size: 13px; }
  .sign span { width: 25%; border-top: 1px solid #444; text-align: center; padding-top: 6px; }
  .vz { width: 100%; border-collapse: collapse; font-size: 12px; margin-bottom: 14px; }
  .vz th, .vz td { border: 1px solid #444; padding: 4px 3px; text-align: center; height: 22px; }
  .vz th { background: #f5f5f5; font-weight: 500; }
  .vz td.left { text-align: left; padding-left: 6px; }
  .vz td.d { width: 4.2%; min-width: 14px; font-variant-numeric: tabular-nums; }
  .vz tr.sub th { padding: 1px 2px; font-size: 10px; }
  .vz tr.total td { font-weight: 500; background: #fafafa; }
  .vz.foot2 td { height: 40px; text-align: left; padding-left: 10px; }
  @media print { body { margin: 12mm; } }
</style>
</head>
<body>${bodyHtml}</body>
</html>`)
  w.document.close()
  w.focus()
  setTimeout(() => w.print(), 300)
}

export function fmtMoney(n) {
  return Number(n || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

// 经典纸质「记帐凭证」打印：摘要 | 总账科目 | 明细科目 | 借方金额 | 贷方金额（含元角分 digit 栏），
// 底部 会计主管/记账/出纳/审核/填制/附单据。entries: [{summary, subject, sub, direction, amount}]
export function printVoucher({ no, date, summary, entries, appendix }) {
  // 金额 digit 栏：亿 千 百 十 万 千 百 十 元 角 分（11 栏）
  const COLS = ['亿', '千', '百', '十', '万', '千', '百', '十', '元', '角', '分']
  const digitCells = (amount) => {
    const cents = Math.round(Number(amount || 0) * 100)
    let digits = String(Math.abs(cents))
    if (digits.length > 11) digits = digits.slice(-11)
    digits = digits.padStart(11, ' ')
    return digits.split('').map((d) => `<td class="d">${d === ' ' ? '' : d}</td>`).join('')
  }
  const head1 = COLS.map((c) => `<th class="d">${c}</th>`).join('')
  const head2 = COLS.map(() => `<th class="d"></th>`).join('')

  let debit = 0, credit = 0
  const rowsHtml = (entries || []).map((e) => {
    if (e.direction === 'debit') debit += Number(e.amount || 0)
    else credit += Number(e.amount || 0)
    return `<tr>
      <td class="left">${e.summary || summary || ''}</td>
      <td>${e.subject || ''}</td>
      <td>${e.sub || ''}</td>
      ${e.direction === 'debit' ? digitCells(e.amount) : head2}
      ${e.direction === 'credit' ? digitCells(e.amount) : head2}
      <td class="d"></td>
    </tr>`
  }).join('')

  const pad = Math.max(0, 4 - (entries || []).length)
  const emptyRow = `<tr><td class="left">&nbsp;</td><td></td><td></td>${head2}${head2}<td class="d"></td></tr>`
  const totalDebit = Math.round(debit * 100)
  const totalCredit = Math.round(credit * 100)
  const balancedTag = totalDebit === totalCredit ? '√' : '不平'

  printHTML('记账凭证', `
    <div style="text-align:center">
      <h1 style="font-size:22px;letter-spacing:8px;margin-bottom:2px">记　帐　凭　证</h1>
      <div style="display:flex;justify-content:space-between;font-size:13px;margin:6px 2px 10px">
        <span>${date ? date.slice(0, 4) : ''}年 ${date ? Number(date.slice(5, 7)) : ''}月 ${date ? Number(date.slice(8, 10)) : ''}日</span>
        <span>字 第 <b>${(no || '').replace(/^[A-Z]+/, '')}</b> 号</span>
      </div>
    </div>
    <table class="vz">
      <tr>
        <th style="width:16%">摘要</th><th style="width:13%">总账科目</th><th style="width:13%">明细科目</th>
        <th colspan="11">借方金额</th><th colspan="11">贷方金额</th><th style="width:4%">记账</th>
      </tr>
      <tr class="sub">${'<th></th>'.repeat(3)}${head1}${head1}<th></th></tr>
      ${rowsHtml}${emptyRow.repeat(pad)}
      <tr class="total">
        <td colspan="3" class="left">合计（${balancedTag}）　￥</td>
        ${digitCells(debit)}${digitCells(credit)}<td class="d"></td>
      </tr>
    </table>
    <table class="vz foot2">
      <tr>
        <td>会计主管</td><td>记账</td><td>出纳</td><td>审核</td><td>填制</td><td>附单据 ${appendix || 0} 张</td>
      </tr>
    </table>`)
}
