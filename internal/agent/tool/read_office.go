package tools

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/fumiama/go-docx"
	"github.com/xuri/excelize/v2"
)

// Office documents are zip containers, so the generic binary probe rejects
// them. The read tool instead parses them to plain text: xlsx via excelize,
// docx via go-docx. Legacy .xls/.doc and pptx stay out of scope (no observed
// demand); they keep falling through to the generic binary handling.

const (
	// readOfficeMaxRenderBytes caps the rendered text so a large workbook or
	// document cannot flood the model context.
	readOfficeMaxRenderBytes = 256 * 1024
	// readOfficeMaxRows caps how many data rows are rendered per file.
	readOfficeMaxRows = 2000
)

// readOfficeContentType reports which office parser the read sniff stage
// should use. Detection needs a zip container plus a known office extension.
func readOfficeContentType(probe []byte, filePath string) (string, bool) {
	if !isZipProbe(probe) {
		return "", false
	}
	switch strings.ToLower(path.Ext(filePath)) {
	case ".xlsx", ".xlsm":
		return "xlsx", true
	case ".docx":
		return "docx", true
	}
	return "", false
}

func isZipProbe(probe []byte) bool {
	if bytes.HasPrefix(probe, []byte("PK\x03\x04")) {
		return true
	}
	return strings.ToLower(strings.TrimSpace(http.DetectContentType(probe))) == "application/zip"
}

func hasZipEntry(data []byte, name string) bool {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}
	for _, file := range reader.File {
		if file.Name == name {
			return true
		}
	}
	return false
}

// readOfficeResult parses an already-loaded office file and returns the same
// shape as a text read, plus row accounting.
func readOfficeResult(filePath, kind string, data []byte) (any, error) {
	var (
		content       string
		totalRows     int
		truncatedRows int
		err           error
	)
	switch kind {
	case "xlsx":
		content, totalRows, truncatedRows, err = renderWorkbook(data)
		if err != nil {
			return nil, fmt.Errorf("workbook could not be parsed: %w", err)
		}
	case "docx":
		content, totalRows, truncatedRows, err = renderDocument(data)
		if err != nil {
			return nil, fmt.Errorf("document could not be parsed: %w", err)
		}
	default:
		return nil, errors.New("unsupported office container")
	}
	result := map[string]any{
		"ok":         true,
		"path":       filePath,
		"size":       len(data),
		"content":    content,
		"total_rows": totalRows,
	}
	if truncatedRows > 0 {
		result["truncated_rows"] = truncatedRows
	}
	return result, nil
}

// readAllAfterProbe drains the bridge reader that already yielded the probe
// bytes, returning the full file content.
func readAllAfterProbe(reader io.Reader, probe []byte) ([]byte, error) {
	var buf bytes.Buffer
	buf.Write(probe)
	if _, err := io.Copy(&buf, reader); err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	return buf.Bytes(), nil
}

// officeRenderer accumulates rendered lines under the shared row and byte
// budgets. Callers account for data rows they skip; the byte budget is
// applied on a line boundary in finish.
type officeRenderer struct {
	lines         []string
	rows          int
	bytes         int
	truncatedRows int
	overBudget    bool
}

func (r *officeRenderer) addLine(line string) {
	r.lines = append(r.lines, line)
	r.bytes += len(line) + 1
	if r.bytes > readOfficeMaxRenderBytes {
		r.overBudget = true
	}
}

// addRow renders one data row and counts it.
func (r *officeRenderer) addRow(line string) {
	r.rows++
	r.addLine(line)
}

func (r *officeRenderer) capped() bool {
	return r.overBudget || r.rows >= readOfficeMaxRows
}

// finish applies the byte budget on a line boundary and appends the
// truncation marker when anything was cut. It returns the content, the total
// data rows the file holds (rendered plus cut), and the cut rows.
func (r *officeRenderer) finish() (string, int, int) {
	content := strings.Join(r.lines, "\n")
	if len(content) > readOfficeMaxRenderBytes {
		cut := content[:readOfficeMaxRenderBytes]
		if idx := strings.LastIndexByte(cut, '\n'); idx >= 0 {
			cut = cut[:idx]
		}
		if r.truncatedRows == 0 {
			// Every data row was rendered but the text itself is too long;
			// report the lines dropped by the byte cut.
			r.truncatedRows = strings.Count(content[len(cut):], "\n")
			if r.truncatedRows == 0 {
				r.truncatedRows = 1
			}
		}
		content = cut
	}
	if r.truncatedRows > 0 {
		content += "\n…(truncated, " + strconv.Itoa(r.truncatedRows) + " more rows)"
	}
	return content, r.rows + r.truncatedRows, r.truncatedRows
}

// officeCellSanitizer keeps one rendered row on one line: office cell text
// may carry tabs and newlines that would corrupt the TSV layout.
var officeCellSanitizer = strings.NewReplacer("\t", " ", "\r", " ", "\n", " ")

// renderWorkbook renders every sheet as a name header plus TSV data rows.
// Sheets after the cap are still fetched so the truncation count is exact.
func renderWorkbook(data []byte) (string, int, int, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return "", 0, 0, err
	}
	defer func() { _ = f.Close() }()

	renderer := &officeRenderer{}
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		if err != nil {
			return "", 0, 0, err
		}
		if renderer.capped() {
			renderer.truncatedRows += len(rows)
			continue
		}
		renderer.addLine("## Sheet: " + sheet)
		for i, row := range rows {
			if renderer.capped() {
				renderer.truncatedRows += len(rows) - i
				break
			}
			cells := make([]string, len(row))
			for j, cell := range row {
				cells[j] = officeCellSanitizer.Replace(cell)
			}
			renderer.addRow(strings.Join(cells, "\t"))
		}
	}
	content, totalRows, truncatedRows := renderer.finish()
	return content, totalRows, truncatedRows, nil
}

// renderDocument renders body paragraphs and tables in document order. The
// container is checked for word/document.xml first: go-docx accepts a zip
// without one and would silently render an empty document.
func renderDocument(data []byte) (string, int, int, error) {
	if !hasZipEntry(data, "word/document.xml") {
		return "", 0, 0, errors.New("missing word/document.xml entry")
	}
	doc, err := docx.Parse(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", 0, 0, err
	}
	items := doc.Document.Body.Items
	renderer := &officeRenderer{}
	for i, item := range items {
		if renderer.capped() {
			renderer.truncatedRows += countDocumentRows(items[i:])
			break
		}
		switch node := item.(type) {
		case *docx.Paragraph:
			text := strings.TrimSpace(node.String())
			if text == "" {
				continue
			}
			renderer.addRow(officeCellSanitizer.Replace(text))
		case *docx.Table:
			renderDocumentTable(renderer, node)
		}
	}
	content, totalRows, truncatedRows := renderer.finish()
	return content, totalRows, truncatedRows, nil
}

func renderDocumentTable(renderer *officeRenderer, table *docx.Table) {
	for i, row := range table.TableRows {
		if renderer.capped() {
			renderer.truncatedRows += len(table.TableRows) - i
			return
		}
		cells := make([]string, 0, len(row.TableCells))
		for _, cell := range row.TableCells {
			parts := make([]string, 0, len(cell.Paragraphs))
			for _, paragraph := range cell.Paragraphs {
				if text := strings.TrimSpace(paragraph.String()); text != "" {
					parts = append(parts, text)
				}
			}
			cells = append(cells, officeCellSanitizer.Replace(strings.Join(parts, " ")))
		}
		renderer.addRow(strings.Join(cells, "\t"))
	}
}

// countDocumentRows counts the data rows a slice of body items represents, so
// the truncation marker is exact.
func countDocumentRows(items []interface{}) int {
	count := 0
	for _, item := range items {
		switch node := item.(type) {
		case *docx.Paragraph:
			if strings.TrimSpace(node.String()) != "" {
				count++
			}
		case *docx.Table:
			count += len(node.TableRows)
		}
	}
	return count
}
