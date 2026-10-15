// Package exchange menangani pertukaran data akunting dengan aplikasi lain:
// menulis dan membaca tabel dalam format .csv, .txt, .xls, .xlsx, dan .xml.
//
// Angka dikirim sebagai bilangan polos bertanda titik desimal (mis. 1250000.00)
// supaya dibaca mesin apa pun, dan pembacaan menerima format Indonesia
// (1.250.000,00) maupun polos. Tanpa pustaka luar: .xlsx ditulis langsung
// sebagai arsip ZIP berisi OOXML, dan .xls memakai SpreadsheetML 2003 yang
// dibuka Excel maupun LibreOffice.
package exchange

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"accounting/internal/models"
)

// Format yang didukung.
const (
	FormatCSV  = "csv"
	FormatTXT  = "txt"
	FormatXLS  = "xls"
	FormatXLSX = "xlsx"
	FormatXML  = "xml"
)

// Formats adalah daftar format beserta labelnya untuk halaman aplikasi.
type Format struct {
	Value string
	Label string
	Mime  string
	Ext   string
}

// AllFormats mengembalikan format yang didukung untuk export dan import.
func AllFormats() []Format {
	return []Format{
		{FormatXLSX, "Excel 2007+ (.xlsx)", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "xlsx"},
		{FormatXLS, "Excel 97-2003 (.xls)", "application/vnd.ms-excel", "xls"},
		{FormatCSV, "Comma separated (.csv)", "text/csv; charset=utf-8", "csv"},
		{FormatTXT, "Tab separated (.txt)", "text/plain; charset=utf-8", "txt"},
		{FormatXML, "XML (.xml)", "application/xml; charset=utf-8", "xml"},
	}
}

// FormatInfo mencari definisi format; ok=false bila tidak dikenal.
func FormatInfo(value string) (Format, bool) {
	for _, f := range AllFormats() {
		if f.Value == value {
			return f, true
		}
	}
	return Format{}, false
}

// Table adalah satu tabel pertukaran data: nama, judul kolom, jenis kolom,
// dan baris.
type Table struct {
	Name       string
	Header     []string
	NumericCol []bool // kolom yang berisi angka
	Rows       [][]string
}

// AddRow menambahkan baris dengan jumlah kolom sesuai header.
func (t *Table) AddRow(values ...string) {
	row := make([]string, len(t.Header))
	copy(row, values)
	t.Rows = append(t.Rows, row)
}

// NewTable membuat tabel beserta jenis kolom.
func NewTable(name string, header []string, numeric []bool) *Table {
	return &Table{Name: name, Header: header, NumericCol: numeric}
}

// Write menulis tabel ke w dengan format yang diminta.
func Write(w io.Writer, t *Table, format string) error {
	switch format {
	case FormatCSV:
		return writeDelimited(w, t, ',')
	case FormatTXT:
		return writeDelimited(w, t, '\t')
	case FormatXML:
		return writeXML(w, t)
	case FormatXLS:
		return writeSpreadsheetML(w, t)
	case FormatXLSX:
		return writeXLSX(w, t)
	default:
		return fmt.Errorf("format %q tidak didukung", format)
	}
}

// writeDelimited menulis CSV (pemisah koma) atau TXT (pemisah tab).
func writeDelimited(w io.Writer, t *Table, comma rune) error {
	cw := csv.NewWriter(w)
	cw.Comma = comma
	if err := cw.Write(t.Header); err != nil {
		return err
	}
	for _, row := range t.Rows {
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// ---- XML ----
//
// Skema pertukaran sengaja sederhana supaya mudah dibaca aplikasi lain:
//
//	<akunting><table name="jurnal">
//	  <columns><col i="0">Tanggal</col>...</columns>
//	  <rows><row><f i="0">01/01/2026</f>...</row></rows>
//	</table></akunting>

type xmlTable struct {
	XMLName xml.Name `xml:"table"`
	Name    string   `xml:"name,attr"`
	Columns []xmlCol `xml:"columns>col"`
	Rows    []xmlRow `xml:"rows>row"`
}

type xmlCol struct {
	Index int    `xml:"i,attr"`
	Title string `xml:",chardata"`
}

type xmlRow struct {
	Fields []xmlField `xml:"f"`
}

type xmlField struct {
	Index int    `xml:"i,attr"`
	Value string `xml:",chardata"`
}

type xmlDoc struct {
	XMLName xml.Name   `xml:"akunting"`
	Tables  []xmlTable `xml:"table"`
}

func writeXML(w io.Writer, t *Table) error {
	doc := xmlDoc{}
	xt := xmlTable{Name: t.Name}
	for i, c := range t.Header {
		xt.Columns = append(xt.Columns, xmlCol{Index: i, Title: c})
	}
	for _, row := range t.Rows {
		xr := xmlRow{}
		for i, v := range row {
			xr.Fields = append(xr.Fields, xmlField{Index: i, Value: v})
		}
		xt.Rows = append(xt.Rows, xr)
	}
	doc.Tables = append(doc.Tables, xt)
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	return enc.Flush()
}

// ---- SpreadsheetML 2003 (.xls) ----

type ssWorkbook struct {
	XMLName    xml.Name     `xml:"Workbook"`
	Xmlns      string       `xml:"xmlns,attr"`
	XmlnsSS    string       `xml:"xmlns:ss,attr"`
	Worksheets []ssWorkheet `xml:"Worksheet"`
}

type ssWorkheet struct {
	Name  string  `xml:"ss:Name,attr"`
	Table ssTable `xml:"Table"`
}

type ssTable struct {
	Rows []ssRow `xml:"Row"`
}

type ssRow struct {
	Cells []ssCell `xml:"Cell"`
}

type ssCell struct {
	Data ssData `xml:"Data"`
}

type ssData struct {
	Type  string `xml:"ss:Type,attr"`
	Value string `xml:",chardata"`
}

func writeSpreadsheetML(w io.Writer, t *Table) error {
	sheet := ssWorkheet{Name: sheetName(t.Name)}
	header := ssRow{}
	for _, c := range t.Header {
		header.Cells = append(header.Cells, ssCell{Data: ssData{Type: "String", Value: c}})
	}
	sheet.Table.Rows = append(sheet.Table.Rows, header)
	for _, row := range t.Rows {
		r := ssRow{}
		for i, v := range row {
			typ := "String"
			if i < len(t.NumericCol) && t.NumericCol[i] {
				typ = "Number"
			}
			r.Cells = append(r.Cells, ssCell{Data: ssData{Type: typ, Value: v}})
		}
		sheet.Table.Rows = append(sheet.Table.Rows, r)
	}
	wb := ssWorkbook{
		Xmlns:      "urn:schemas-microsoft-com:office:spreadsheet",
		XmlnsSS:    "urn:schemas-microsoft-com:office:spreadsheet",
		Worksheets: []ssWorkheet{sheet},
	}
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	if _, err := io.WriteString(w, `<?mso-application progid="Excel.Sheet"?>`+"\n"); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	if err := enc.Encode(wb); err != nil {
		return err
	}
	return enc.Flush()
}

// ---- XLSX (OOXML di dalam ZIP) ----

const xlsxContentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>
</Types>`

const xlsxRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`

const xlsxWorkbookRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`

// xlsxStyles menyediakan dua gaya sel: umum dan angka dengan pemisah ribuan.
const xlsxStyles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<numFmts count="1"><numFmt numFmtId="164" formatCode="#,##0.00"/></numFmts>
<fonts count="1"><font><sz val="11"/><name val="Calibri"/></font></fonts>
<fills count="1"><fill><patternFill patternType="none"/></fill></fills>
<borders count="1"><border/></borders>
<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>
<cellXfs count="2">
<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>
<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
</cellXfs>
</styleSheet>`

func writeXLSX(w io.Writer, t *Table) error {
	zw := zip.NewWriter(w)
	defer zw.Close()

	parts := []struct {
		name string
		body string
	}{
		{"[Content_Types].xml", xlsxContentTypes},
		{"_rels/.rels", xlsxRels},
		{"xl/workbook.xml", xlsxWorkbook(t.Name)},
		{"xl/_rels/workbook.xml.rels", xlsxWorkbookRels},
		{"xl/styles.xml", xlsxStyles},
		{"xl/worksheets/sheet1.xml", xlsxSheet(t)},
	}
	for _, p := range parts {
		f, err := zw.Create(p.name)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(f, p.body); err != nil {
			return err
		}
	}
	return nil
}

func xlsxWorkbook(tableName string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets><sheet name="` + xmlEscape(sheetName(tableName)) + `" sheetId="1" r:id="rId1"/></sheets>
</workbook>`
}

func xlsxSheet(t *Table) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	rowNum := 1
	writeRow := func(cells []string) {
		b.WriteString(`<row r="` + strconv.Itoa(rowNum) + `">`)
		for i, v := range cells {
			ref := columnLetter(i+1) + strconv.Itoa(rowNum)
			numeric := i < len(t.NumericCol) && t.NumericCol[i] && v != ""
			if numeric {
				b.WriteString(`<c r="` + ref + `" s="1"><v>` + xmlEscape(v) + `</v></c>`)
				continue
			}
			b.WriteString(`<c r="` + ref + `" t="inlineStr"><is><t xml:space="preserve">` + xmlEscape(v) + `</t></is></c>`)
		}
		b.WriteString(`</row>`)
		rowNum++
	}
	writeRow(t.Header)
	for _, row := range t.Rows {
		writeRow(row)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String()
}

// columnLetter mengubah nomor kolom (1 = A) menjadi huruf kolom Excel.
func columnLetter(n int) string {
	var out []byte
	for n > 0 {
		n--
		out = append([]byte{byte('A' + n%26)}, out...)
		n /= 26
	}
	return string(out)
}

// columnIndex adalah kebalikan columnLetter ("A" = 0).
func columnIndex(letters string) int {
	n := 0
	for i := 0; i < len(letters); i++ {
		c := letters[i]
		if c < 'A' || c > 'Z' {
			break
		}
		n = n*26 + int(c-'A'+1)
	}
	return n - 1
}

func xmlEscape(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// sheetName membersihkan nama tabel agar sah menjadi nama sheet Excel
// (maksimal 31 karakter, tanpa karakter terlarang).
func sheetName(name string) string {
	bad := []string{":", "\\", "/", "?", "*", "[", "]", "'"}
	for _, b := range bad {
		name = strings.ReplaceAll(name, b, "-")
	}
	if name == "" {
		return "data"
	}
	if len(name) > 31 {
		name = name[:31]
	}
	return name
}

// moneyColumn mengubah sen menjadi teks angka polos bertitik desimal.
func moneyColumn(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return sign + strconv.FormatInt(cents/100, 10) + "." + fmt.Sprintf("%02d", cents%100)
}

// MoneyText memformat sen sebagai angka polos untuk pertukaran data.
func MoneyText(cents int64) string { return moneyColumn(cents) }

// ParseMoneyColumn membaca angka polos maupun format Indonesia menjadi sen.
func ParseMoneyColumn(s string) (int64, error) { return models.ParseMoney(s) }

// ParseTanggal membaca tanggal format Indonesia maupun ISO.
func ParseTanggal(s string) (time.Time, error) { return models.ParseDateID(s) }

// FormatTanggal menulis tanggal sebagai dd/mm/yyyy.
func FormatTanggal(t time.Time) string { return t.Format("02/01/2006") }

// sortRows mengurutkan baris secara stabil berdasarkan kolom tertentu.
func sortRows(rows [][]string, col int) {
	sort.SliceStable(rows, func(i, j int) bool {
		if col >= len(rows[i]) || col >= len(rows[j]) {
			return false
		}
		return rows[i][col] < rows[j][col]
	})
}

// bufferTable menulis tabel ke buffer memori (dipakai untuk uji dan template).
func bufferTable(t *Table, format string) ([]byte, error) {
	var buf bytes.Buffer
	if err := Write(&buf, t, format); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
