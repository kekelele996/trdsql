package trdsql

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNewImporter(t *testing.T) {
	type args struct {
		inputFormat Format
	}
	tests := []struct {
		name string
		args args
		want Format
	}{
		{
			name: "test1",
			args: args{inputFormat: CSV},
			want: CSV,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewImporter(InFormat(CSV)); !reflect.DeepEqual(got.ReadOpts.InFormat, tt.want) {
				t.Errorf("NewImporter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestImporter_Import(t *testing.T) {
	type fields struct {
		ReadOpts *ReadOpts
	}
	tests := []struct {
		name    string
		fields  fields
		query   string
		want    string
		wantErr bool
	}{
		{
			name:    "test1",
			fields:  fields{ReadOpts: NewReadOpts()},
			query:   "SELECT 1",
			want:    "SELECT 1",
			wantErr: false,
		},
		{
			name:    "testNoFile",
			fields:  fields{ReadOpts: NewReadOpts()},
			query:   "SELECT * FROM ttttnofile.csv",
			want:    "SELECT * FROM ttttnofile.csv",
			wantErr: false,
		},
		{
			name:    "testGenerate_series",
			fields:  fields{ReadOpts: NewReadOpts()},
			query:   "SELECT * FROM generate_series(1,10)",
			want:    "SELECT * FROM generate_series(1,10)",
			wantErr: false,
		},
		{
			name:    "testGenerate_series2",
			fields:  fields{ReadOpts: NewReadOpts()},
			query:   "SELECT * FROM generate_series(1,10,2)",
			want:    "SELECT * FROM generate_series(1,10,2)",
			wantErr: false,
		},
		{
			name:    "test3",
			fields:  fields{ReadOpts: NewReadOpts(InDelimiter("ddd"))},
			query:   "SELECT * FROM testdata/test.csv",
			want:    "SELECT * FROM testdata/test.csv",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := Connect(DefaultDriver, "")
			if err != nil {
				t.Fatal(err)
			}
			db.Tx, err = db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			i := &ReadFormat{
				ReadOpts: tt.fields.ReadOpts,
			}
			got, err := i.Import(db, tt.query)
			if (err != nil) != tt.wantErr {
				t.Errorf("Importer.Import() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Importer.Import() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_listTable(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantTbl map[string]string
		wantIdx []int
	}{
		{
			name:    "noTable",
			query:   "SELECT 1;",
			wantTbl: map[string]string{},
			wantIdx: []int{},
		},
		{
			name:    "testTable",
			query:   "SELECT * FROM test;",
			wantTbl: map[string]string{"test": "test"},
			wantIdx: []int{6},
		},
		{
			name:    "testJoin",
			query:   "SELECT test.a FROM test LEFT JOIN test2 ON (test.b = test2.b);",
			wantTbl: map[string]string{"test": "test", "test2": "test2"},
			wantIdx: []int{6, 12},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTbl, gotIdx := TableNames(SQLFields(tt.query))
			if !reflect.DeepEqual(gotTbl, tt.wantTbl) {
				t.Errorf("TableNames() Table Map = %v, want %v", gotTbl, tt.wantTbl)
			}
			if !reflect.DeepEqual(gotIdx, tt.wantIdx) {
				t.Errorf("TableNames() Table Index = %v, want %v", gotIdx, tt.wantIdx)
			}
		})
	}
}

func Test_SQLFields(t *testing.T) {
	type args struct {
		query string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "testNo",
			args: args{query: ""},
			want: []string{},
		},
		{
			name: "testDoubleQuote",
			args: args{query: `SELECT * FROM "C:\file with a space.csv"`},
			want: []string{`SELECT`, ` `, `*`, ` `, `FROM`, ` `, `"C:\file with a space.csv"`},
		},
		{
			name: "testSingleQuote",
			args: args{query: `SELECT * FROM 'C:\file with a space.csv'`},
			want: []string{`SELECT`, ` `, `*`, ` `, `FROM`, ` `, `'C:\file with a space.csv'`},
		},
		{
			name: "testSingleQuote2",
			args: args{query: "SELECT * FROM jame's.csv"},
			want: []string{"SELECT", " ", "*", " ", "FROM", " ", "jame's.csv"},
		},
		{
			name: "testBackQuote",
			args: args{query: "SELECT * FROM `C:\file with a space.csv`;"},
			want: []string{`SELECT`, ` `, `*`, ` `, `FROM`, ` `, "`C:\file with a space.csv`", `;`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SQLFields(tt.args.query); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SQLFields() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func newDBTestSqlite3() *DB {
	db, err := Connect(DefaultDriver, "")
	if err != nil {
		return nil
	}
	return db
}

func newDBTestPostgres() *DB {
	db, err := Connect("postgres", pgDsn())
	if err != nil {
		return nil
	}
	if err := db.Ping(); err != nil {
		return nil
	}
	return db
}

func newDBTestMysql() *DB {
	db, err := Connect("mysql", myDsn())
	if err != nil {
		return nil
	}
	if err = db.Ping(); err != nil {
		return nil
	}
	return db
}

func csvReadOpts() *ReadOpts {
	opts := NewReadOpts()
	opts.InFormat = CSV
	return opts
}

func TestImportFile(t *testing.T) {
	type args struct {
		db       *DB
		fileName string
		opts     *ReadOpts
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "testNoFile",
			args: args{
				db:       newDBTestSqlite3(),
				fileName: "nofile",
				opts:     NewReadOpts(),
			},
			want:    "",
			wantErr: false,
		},
		{
			name: "testGlobFile",
			args: args{
				db:       newDBTestSqlite3(),
				fileName: "testdata/test*.csv",
				opts:     NewReadOpts(),
			},
			want:    "`testdata/test*.csv`",
			wantErr: false,
		},
		{
			name: "testNoMatch",
			args: args{
				db:       newDBTestSqlite3(),
				fileName: "testdata/testtttttt*.csv",
				opts:     NewReadOpts(),
			},
			want:    "",
			wantErr: false,
		},
		{
			name: "testCSV",
			args: args{
				db:       newDBTestSqlite3(),
				fileName: "testdata/test.csv",
				opts:     csvReadOpts(),
			},
			want:    "`testdata/test.csv`",
			wantErr: false,
		},
		{
			name: "testSqlite",
			args: args{
				db:       newDBTestSqlite3(),
				fileName: "testdata/test.csv",
				opts:     NewReadOpts(),
			},
			want:    "`testdata/test.csv`",
			wantErr: false,
		},
		{
			name: "testPostgres",
			args: args{
				db:       newDBTestPostgres(),
				fileName: "testdata/test.csv",
				opts:     NewReadOpts(),
			},
			want:    "\"testdata/test.csv\"",
			wantErr: false,
		},
		{
			name: "testMysql",
			args: args{
				db:       newDBTestMysql(),
				fileName: "testdata/test.csv",
				opts:     NewReadOpts(),
			},
			want:    "`testdata/test.csv`",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.args.db == nil {
				t.SkipNow()
			}
			var err error
			tt.args.db.Tx, err = tt.args.db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			got, err := ImportFile(tt.args.db, tt.args.fileName, tt.args.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("ImportFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ImportFile() = %v, want %v", got, tt.want)
			}
			if err = tt.args.db.Tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if err = tt.args.db.Disconnect(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func Test_guessFormat(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
		want      Format
	}{
		{name: "testCSV", tableName: "test.csv", want: CSV},
		{name: "testLTSV", tableName: "test.ltsv", want: LTSV},
		{name: "testTSV", tableName: "test.tsv", want: TSV},
		{name: "testPSV", tableName: "test.psv", want: PSV},
		{name: "testgz", tableName: "test.ltsv.gz", want: LTSV},
		{name: "testzstd", tableName: "test.ltsv.zst", want: LTSV},
		{name: "testlz4", tableName: "test.ltsv.lz4", want: LTSV},
		{name: "testbzip2", tableName: "test.ltsv.bz2", want: LTSV},
		{name: "testJSON", tableName: "test.json", want: JSON},
		{name: "testTBLN", tableName: "test.tbln", want: TBLN},
		{name: "testunknown", tableName: "test.go", want: CSV},
		{name: "testunknown2", tableName: "testltsv", want: CSV},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := guessFormat(tt.tableName); got != tt.want {
				t.Errorf("guessFormat() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_trimQuote(t *testing.T) {
	type args struct {
		fileName string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "test1",
			args: args{"test"},
			want: "test",
		},
		{
			name: "test2",
			args: args{"`test`"},
			want: "test",
		},
		{
			name: "test3",
			args: args{"\"test\""},
			want: "test",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trimQuote(tt.args.fileName); got != tt.want {
				t.Errorf("trimQuote() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_uncompressedReader(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		want     string
	}{
		{
			name:     "testGzFile",
			fileName: filepath.Join(dataDir, "test.csv.gz"),
			want:     "1,Orange\n2,Melon\n3,Apple\n",
		},
		{
			name:     "testBzip2File",
			fileName: filepath.Join(dataDir, "test.csv.bz2"),
			want:     "1,Orange\n2,Melon\n3,Apple\n",
		},
		{
			name:     "testZSTDFile",
			fileName: filepath.Join(dataDir, "test.csv.zst"),
			want:     "1,Orange\n2,Melon\n3,Apple\n",
		},
		{
			name:     "testLZ4File",
			fileName: filepath.Join(dataDir, "test.csv.lz4"),
			want:     "1,Orange\n2,Melon\n3,Apple\n",
		},
		{
			name:     "testXZFile",
			fileName: filepath.Join(dataDir, "test.csv.xz"),
			want:     "1,Orange\n2,Melon\n3,Apple\n",
		},
		{
			name:     "testNoGzFile",
			fileName: filepath.Join(dataDir, "testNoGzFile.gz"),
			want:     "1,Orange\n2,Melon\n3,Apple",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := os.Open(tt.fileName)
			if err != nil {
				t.Fatalf("extFileReader() file open error %s:%s", tt.fileName, err)
			}
			got, err := uncompressedReader(file)
			if err != nil {
				t.Fatalf("extFileReader() uncompressed error %s:%s", tt.fileName, err)
			}
			r, err := io.ReadAll(got)
			if err != nil {
				t.Fatalf("extFileReader() read error %s:%s", tt.fileName, err)
			}
			if !reflect.DeepEqual(string(r)[0:7], tt.want[0:7]) {
				t.Errorf("extFileReader() = %v, want %v", string(r)[0:7], tt.want[0:7])
			}
		})
	}
}

// corruptGzipBytes returns a gzip stream with a valid header
// that is truncated, so reading it to the end fails.
func corruptGzipBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte("1,Orange\n2,Melon\n")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	// Remove the trailer (CRC32 and size): header stays valid.
	return b[:len(b)-8]
}

func TestImportFileGlobCorrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.csv"), []byte("1,Orange\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	badFile := filepath.Join(dir, "b.csv.gz")
	if err := os.WriteFile(badFile, corruptGzipBytes(t), 0o644); err != nil {
		t.Fatal(err)
	}

	db := newDBTestSqlite3()
	if db == nil {
		t.Skip("sqlite3 is not available")
	}
	var err error
	db.Tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}

	got, err := ImportFile(db, filepath.Join(dir, "*.csv*"), NewReadOpts())
	if err == nil {
		t.Fatal("expected an error for a glob containing a corrupt archive")
	}
	if !strings.Contains(err.Error(), badFile) {
		t.Errorf("error should name the failing file %q: %v", badFile, err)
	}
	// The error surfaces during import after the table is created,
	// so the quoted table name is returned together with the error.
	if want := "`" + filepath.Join(dir, "*.csv*") + "`"; got != want {
		t.Errorf("table name = %q, want %q", got, want)
	}

	if rollbackErr := db.Tx.Rollback(); rollbackErr != nil {
		t.Fatal(rollbackErr)
	}
	if disconnectErr := db.Disconnect(); disconnectErr != nil {
		t.Fatal(disconnectErr)
	}
}

func TestImportFileGlobPermission(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.csv"), []byte("1,Orange\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	badFile := filepath.Join(dir, "b.csv")
	if err := os.WriteFile(badFile, []byte("2,Melon\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(badFile, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(badFile, 0o644) })

	db := newDBTestSqlite3()
	if db == nil {
		t.Skip("sqlite3 is not available")
	}
	var err error
	db.Tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}

	_, err = ImportFile(db, filepath.Join(dir, "*.csv"), NewReadOpts())
	if err == nil {
		t.Fatal("expected an error for a glob containing an unreadable file")
	}
	if !strings.Contains(err.Error(), badFile) {
		t.Errorf("error should name the failing file %q: %v", badFile, err)
	}

	if rollbackErr := db.Tx.Rollback(); rollbackErr != nil {
		t.Fatal(rollbackErr)
	}
	if disconnectErr := db.Disconnect(); disconnectErr != nil {
		t.Fatal(disconnectErr)
	}
}

// TestTRDSQLGlobErrorNoOutput verifies end-to-end that a read error
// on one file fails the query, names the file, and emits no output.
func TestTRDSQLGlobErrorNoOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.csv"), []byte("1,Orange\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	badFile := filepath.Join(dir, "b.csv.gz")
	if err := os.WriteFile(badFile, corruptGzipBytes(t), 0o644); err != nil {
		t.Fatal(err)
	}

	out := new(bytes.Buffer)
	trd := setDefaultTRDSQL(out)
	err := trd.Exec("SELECT * FROM " + filepath.Join(dir, "*.csv*"))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), badFile) {
		t.Errorf("error should name the failing file %q: %v", badFile, err)
	}
	if out.Len() != 0 {
		t.Errorf("no output expected on failure, got %q", out.String())
	}
}

// TestTRDSQLStdin verifies that reading from stdin still works.
func TestTRDSQLStdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = oldStdin })

	go func() {
		_, _ = w.Write([]byte("1,Orange\n"))
		_ = w.Close()
	}()

	out := new(bytes.Buffer)
	trd := setDefaultTRDSQL(out)
	if err := trd.Exec("SELECT * FROM -"); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "1,Orange\n"; got != want {
		t.Errorf("stdin result = %q, want %q", got, want)
	}
}
