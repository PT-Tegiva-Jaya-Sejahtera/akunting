package templates

// Berkas ini memuat tipe data halaman export/import. Dipisahkan dari berkas
// .templ supaya templ hanya berisi markup.

// ExchangeDataset adalah satu pilihan data yang bisa diekspor/diimpor.
type ExchangeDataset struct {
	Value      string
	Label      string
	Desc       string
	Importable bool
}

// ExchangeFormat adalah satu format berkas pertukaran data.
type ExchangeFormat struct {
	Value string
	Label string
	Ext   string
}

// ExchangeView adalah data halaman Export / Import.
type ExchangeView struct {
	Datasets    []ExchangeDataset
	Formats     []ExchangeFormat
	Importable  []ExchangeDataset
	TodayLabel  string
}

// ExchangePreviewView adalah data halaman pemeriksaan berkas import.
type ExchangePreviewView struct {
	FileName  string
	Dataset   string
	Label     string
	Format    string
	TotalRows int
	Groups    int
	Valid     int
	Errors    []string
	Warnings  []string
	Token     string
	Datasets  []ExchangeDataset
	Formats   []ExchangeFormat
}

// ExchangeResultView adalah data halaman hasil penerapan import.
type ExchangeResultView struct {
	FileName string
	Dataset  string
	Applied  bool
	Count    int
	Errors   []string
	Warnings []string
}
