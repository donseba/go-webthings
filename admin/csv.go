package admin

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

// CSVRow is one row of a CSV file, with the line it starts on.
type CSVRow struct {
	Line   int
	Values []string
}

// byteOrderMark starts the CSV files of many spreadsheet programs.
var byteOrderMark = []byte("\xef\xbb\xbf")

// ReadCSV reads every row of a CSV file made with a spreadsheet program, for
// an import. A byte order mark is skipped. The separator is whichever of
// comma, semicolon or tab the first line uses most, because spreadsheets in
// many countries save CSV with semicolons. Rows may have different numbers of
// values, spaces before a value are dropped and a stray quote is kept as
// written. It fails when source cannot be read, or with a *csv.ParseError,
// which names the line, when a row is not CSV at all.
func ReadCSV(source io.Reader) ([]CSVRow, error) {
	data, err := io.ReadAll(source)
	if err != nil {
		return nil, err
	}

	data = bytes.TrimPrefix(data, byteOrderMark)

	reader := csv.NewReader(bytes.NewReader(data))
	reader.Comma = separator(data)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true

	var rows []CSVRow

	for {
		values, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return rows, nil
		}

		if err != nil {
			return nil, err
		}

		line, _ := reader.FieldPos(0)
		rows = append(rows, CSVRow{Line: line, Values: values})
	}
}

// separator is the one of comma, semicolon and tab that the first line of
// data uses most; comma when there is a tie.
func separator(data []byte) rune {
	header, _, _ := bytes.Cut(data, []byte("\n"))
	chosen := ','

	for _, candidate := range []rune{';', '\t'} {
		if bytes.Count(header, []byte(string(candidate))) > bytes.Count(header, []byte(string(chosen))) {
			chosen = candidate
		}
	}

	return chosen
}

// CSVCell keeps spreadsheet programs from running a value of an export as a
// formula: a value that starts with =, +, -, @, a tab or a carriage return
// gets a leading apostrophe, which spreadsheets show as text.
func CSVCell(value string) string {
	if value != "" && strings.ContainsRune("=+-@\t\r", rune(value[0])) {
		return "'" + value
	}

	return value
}

// WriteCSV answers with a CSV file to download, named name plus ".csv": the
// header row and then rows. Every value of rows goes through CSVCell, since
// exports hold what visitors typed. The file is never cached, as exports
// usually hold personal data.
func WriteCSV(w http.ResponseWriter, name string, header []string, rows [][]string) error {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + ".csv"}))
	w.Header().Set("Cache-Control", "no-store")

	out := csv.NewWriter(w)

	if err := out.Write(header); err != nil {
		return err
	}

	for _, row := range rows {
		cells := make([]string, len(row))
		for i, value := range row {
			cells[i] = CSVCell(value)
		}

		if err := out.Write(cells); err != nil {
			return err
		}
	}

	out.Flush()

	return out.Error()
}
