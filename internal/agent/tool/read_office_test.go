package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/fumiama/go-docx"
	"github.com/xuri/excelize/v2"
)

// buildTestWorkbook renders a two-sheet workbook with Chinese headers into
// xlsx bytes using the same library the read branch parses with.
func buildTestWorkbook(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	sheet1 := f.GetSheetName(0)
	if err := f.SetSheetName(sheet1, "数据表"); err != nil {
		t.Fatalf("rename sheet: %v", err)
	}
	for i, row := range [][]any{
		{"名称", "数量", "备注"},
		{"苹果", 3, "新鲜"},
		{"香蕉", 12, ""},
	} {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		if err := f.SetSheetRow("数据表", cell, &row); err != nil {
			t.Fatalf("set row %d: %v", i, err)
		}
	}
	if _, err := f.NewSheet("汇总"); err != nil {
		t.Fatalf("new sheet: %v", err)
	}
	if err := f.SetCellValue("汇总", "A1", "总计"); err != nil {
		t.Fatalf("set summary cell: %v", err)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write workbook: %v", err)
	}
	return buf.Bytes()
}

// buildTestDocx renders a document with paragraphs and a table into docx bytes.
func buildTestDocx(t *testing.T) []byte {
	t.Helper()
	d := docx.New()
	p := d.AddParagraph()
	p.AddText("项目周报")
	p2 := d.AddParagraph()
	p2.AddText("本周完成了三件事。")
	table := d.AddTable(2, 2, 0, nil)
	table.TableRows[0].TableCells[0].AddParagraph().AddText("任务")
	table.TableRows[0].TableCells[1].AddParagraph().AddText("状态")
	table.TableRows[1].TableCells[0].AddParagraph().AddText("写文档")
	table.TableRows[1].TableCells[1].AddParagraph().AddText("已完成")
	var buf bytes.Buffer
	if _, err := d.WriteTo(&buf); err != nil {
		t.Fatalf("write docx: %v", err)
	}
	return buf.Bytes()
}

// buildZipWithEntry builds a zip container that is not an office document.
func buildZipWithEntry(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	entry, err := w.Create("readme.txt")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := entry.Write([]byte("not an office document")); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestReadOfficeXlsxRendersRows(t *testing.T) {
	t.Parallel()

	provider := NewContainerProvider(nil, containerTestBridgeProvider{client: newReadMediaTestClient(t, map[string][]byte{
		"uploads/sheet.xlsx": buildTestWorkbook(t),
	})}, nil, "")

	result, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1"}, readArgs{Path: "/data/uploads/sheet.xlsx"})
	if err != nil {
		t.Fatalf("execRead() error = %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	content, _ := m["content"].(string)
	for _, want := range []string{"数据表", "汇总", "名称\t数量\t备注", "苹果\t3\t新鲜", "总计"} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected content to contain %q, got:\n%s", want, content)
		}
	}
	if !strings.Contains(content, "香蕉\t12") {
		t.Fatalf("expected second data row, got:\n%s", content)
	}
	if got, _ := m["total_rows"].(int); got != 4 {
		t.Fatalf("expected total_rows=4 (3 data rows + 1 summary row), got %v", m["total_rows"])
	}
	if _, truncated := m["truncated_rows"]; truncated {
		t.Fatalf("small workbook must not report truncation, got %v", m["truncated_rows"])
	}
}

func TestReadOfficeXlsxTruncatesRows(t *testing.T) {
	t.Parallel()

	f := excelize.NewFile()
	for row := 1; row <= readOfficeMaxRows+500; row++ {
		cell, _ := excelize.CoordinatesToCellName(1, row)
		if err := f.SetCellValue("Sheet1", cell, row); err != nil {
			t.Fatalf("set cell %s: %v", cell, err)
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write workbook: %v", err)
	}

	provider := NewContainerProvider(nil, containerTestBridgeProvider{client: newReadMediaTestClient(t, map[string][]byte{
		"uploads/big.xlsx": buf.Bytes(),
	})}, nil, "")

	result, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1"}, readArgs{Path: "/data/uploads/big.xlsx"})
	if err != nil {
		t.Fatalf("execRead() error = %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	content, _ := m["content"].(string)
	if got, _ := m["total_rows"].(int); got != readOfficeMaxRows+500 {
		t.Fatalf("expected total_rows=%d, got %v", readOfficeMaxRows+500, m["total_rows"])
	}
	if got, _ := m["truncated_rows"].(int); got != 500 {
		t.Fatalf("expected truncated_rows=500, got %v", m["truncated_rows"])
	}
	if !strings.Contains(content, "…(truncated, 500 more rows)") {
		t.Fatalf("expected truncation marker, got tail:\n%s", content[len(content)-200:])
	}
	if got := strings.Count(content, "\n"); got > readOfficeMaxRows+16 {
		t.Fatalf("rendered %d lines for a %d-row cap", got, readOfficeMaxRows)
	}
}

func TestReadOfficeXlsxTruncatesRenderedBytes(t *testing.T) {
	t.Parallel()

	f := excelize.NewFile()
	// Few rows, huge cells: the byte budget must cut before the row cap.
	bigCell := strings.Repeat("长", 4096)
	for row := 1; row <= 100; row++ {
		cell, _ := excelize.CoordinatesToCellName(1, row)
		if err := f.SetCellValue("Sheet1", cell, bigCell); err != nil {
			t.Fatalf("set cell %s: %v", cell, err)
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write workbook: %v", err)
	}

	provider := NewContainerProvider(nil, containerTestBridgeProvider{client: newReadMediaTestClient(t, map[string][]byte{
		"uploads/wide.xlsx": buf.Bytes(),
	})}, nil, "")

	result, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1"}, readArgs{Path: "/data/uploads/wide.xlsx"})
	if err != nil {
		t.Fatalf("execRead() error = %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	content, _ := m["content"].(string)
	if len(content) > readOfficeMaxRenderBytes+64 {
		t.Fatalf("rendered %d bytes, budget is %d", len(content), readOfficeMaxRenderBytes)
	}
	if got, _ := m["truncated_rows"].(int); got <= 0 {
		t.Fatalf("expected truncated_rows > 0, got %v", m["truncated_rows"])
	}
	if !strings.Contains(content, "…(truncated,") {
		t.Fatalf("expected truncation marker, got tail:\n%s", content[len(content)-200:])
	}
}

func TestReadOfficeDocxRendersText(t *testing.T) {
	t.Parallel()

	provider := NewContainerProvider(nil, containerTestBridgeProvider{client: newReadMediaTestClient(t, map[string][]byte{
		"uploads/report.docx": buildTestDocx(t),
	})}, nil, "")

	result, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1"}, readArgs{Path: "/data/uploads/report.docx"})
	if err != nil {
		t.Fatalf("execRead() error = %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	content, _ := m["content"].(string)
	for _, want := range []string{"项目周报", "本周完成了三件事。", "任务\t状态", "写文档\t已完成"} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected content to contain %q, got:\n%s", want, content)
		}
	}
}

func TestReadOfficeDocxTruncatesRenderedBytes(t *testing.T) {
	t.Parallel()

	d := docx.New()
	paragraphCount := 200
	longText := strings.Repeat("字", 4096)
	for i := 0; i < paragraphCount; i++ {
		d.AddParagraph().AddText(longText)
	}
	var buf bytes.Buffer
	if _, err := d.WriteTo(&buf); err != nil {
		t.Fatalf("write docx: %v", err)
	}

	provider := NewContainerProvider(nil, containerTestBridgeProvider{client: newReadMediaTestClient(t, map[string][]byte{
		"uploads/long.docx": buf.Bytes(),
	})}, nil, "")

	result, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1"}, readArgs{Path: "/data/uploads/long.docx"})
	if err != nil {
		t.Fatalf("execRead() error = %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	content, _ := m["content"].(string)
	if len(content) > readOfficeMaxRenderBytes+64 {
		t.Fatalf("rendered %d bytes, budget is %d", len(content), readOfficeMaxRenderBytes)
	}
	if !strings.Contains(content, "…(truncated,") {
		t.Fatalf("expected truncation marker, got tail:\n%s", content[len(content)-200:])
	}
}

func TestReadOfficeParseFailures(t *testing.T) {
	t.Parallel()

	t.Run("zip with xlsx extension is not a workbook", func(t *testing.T) {
		t.Parallel()
		provider := NewContainerProvider(nil, containerTestBridgeProvider{client: newReadMediaTestClient(t, map[string][]byte{
			"uploads/fake.xlsx": buildZipWithEntry(t),
		})}, nil, "")
		_, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1"}, readArgs{Path: "/data/uploads/fake.xlsx"})
		if err == nil {
			t.Fatal("expected parse failure")
		}
		if !strings.Contains(err.Error(), "workbook could not be parsed") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("zip with docx extension is not a document", func(t *testing.T) {
		t.Parallel()
		provider := NewContainerProvider(nil, containerTestBridgeProvider{client: newReadMediaTestClient(t, map[string][]byte{
			"uploads/fake.docx": buildZipWithEntry(t),
		})}, nil, "")
		_, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1"}, readArgs{Path: "/data/uploads/fake.docx"})
		if err == nil {
			t.Fatal("expected parse failure")
		}
		if !strings.Contains(err.Error(), "document could not be parsed") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestReadOfficeSniffRouting(t *testing.T) {
	t.Parallel()

	t.Run("xlsx routes to office branch without image support", func(t *testing.T) {
		t.Parallel()
		provider := NewContainerProvider(nil, containerTestBridgeProvider{client: newReadMediaTestClient(t, map[string][]byte{
			"uploads/sheet.xlsx": buildTestWorkbook(t),
		})}, nil, "")
		result, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1", SupportsImageInput: false}, readArgs{Path: "/data/uploads/sheet.xlsx"})
		if err != nil {
			t.Fatalf("execRead() error = %v", err)
		}
		if m, ok := result.(map[string]any); !ok || m["content"] == nil {
			t.Fatalf("expected office text result, got %v", result)
		}
	})

	t.Run("generic zip stays on the binary rejection path", func(t *testing.T) {
		t.Parallel()
		provider := NewContainerProvider(nil, containerTestBridgeProvider{client: newReadMediaTestClient(t, map[string][]byte{
			"uploads/bundle.zip": buildZipWithEntry(t),
		})}, nil, "")
		result, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1", SupportsImageInput: true}, readArgs{Path: "/data/uploads/bundle.zip"})
		if err != nil {
			t.Fatalf("execRead() error = %v", err)
		}
		media, ok := result.(ReadMediaToolOutput)
		if !ok {
			t.Fatalf("expected generic image-lane rejection, got %T", result)
		}
		if media.Public.OK || !strings.Contains(media.Public.Error, "PNG") {
			t.Fatalf("expected unsupported-mime rejection, got %+v", media.Public)
		}
	})

	t.Run("pptx is not claimed by the office branch", func(t *testing.T) {
		t.Parallel()
		// O-3: pptx stays out of scope; a zip container with .pptx extension
		// must fall through to the generic binary path, not the office parse.
		provider := NewContainerProvider(nil, containerTestBridgeProvider{client: newReadMediaTestClient(t, map[string][]byte{
			"uploads/deck.pptx": buildZipWithEntry(t),
		})}, nil, "")
		result, err := provider.execRead(context.Background(), SessionContext{BotID: "bot-1", SupportsImageInput: true}, readArgs{Path: "/data/uploads/deck.pptx"})
		if err != nil {
			t.Fatalf("execRead() error = %v", err)
		}
		media, ok := result.(ReadMediaToolOutput)
		if !ok {
			t.Fatalf("expected generic image-lane rejection, got %T", result)
		}
		if media.Public.OK || !strings.Contains(media.Public.Error, "PNG") {
			t.Fatalf("expected unsupported-mime rejection, got %+v", media.Public)
		}
	})
}

func TestReadDescriptionMentionsOfficeDocuments(t *testing.T) {
	t.Parallel()

	desc := readToolDescription(t, false)
	if !strings.Contains(desc, "Spreadsheet (.xlsx)") || !strings.Contains(desc, "Word (.docx)") {
		t.Fatalf("expected read description to mention office coverage, got:\n%s", desc)
	}
}
