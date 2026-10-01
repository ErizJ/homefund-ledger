// Excel 导出工具：基于 SheetJS，把二维表导出为 .xlsx
import * as XLSX from 'xlsx'

/**
 * @param {string} filename 下载文件名（建议带 .xlsx 后缀）
 * @param {string} sheetName 工作表名（不超过 31 字符）
 * @param {string[]} headers 列头
 * @param {Array<Array<string|number>>} rows 数据行（合计行等直接拼在末尾）
 */
export function exportExcel(filename, sheetName, headers, rows) {
  const ws = XLSX.utils.aoa_to_sheet([headers, ...rows])
  const wb = XLSX.utils.book_new()
  XLSX.utils.book_append_sheet(wb, ws, sheetName.slice(0, 31))
  XLSX.writeFile(wb, filename)
}
