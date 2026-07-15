// service/invoice.go
package service

import (
	"bytes"
	"fmt"
	"time"

	"chronosphere/domain"

	"github.com/go-pdf/fpdf"
)

func GenerateInvoicePDF(payment *domain.Payment, student *domain.User, pkg *domain.Package) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Arial", "B", 16)
	pdf.Cell(0, 10, "INVOICE")
	pdf.Ln(12)

	pdf.SetFont("Arial", "", 11)
	pdf.Cell(0, 7, fmt.Sprintf("No. Invoice: %s", payment.ExternalID))
	pdf.Ln(6)
	pdf.Cell(0, 7, fmt.Sprintf("Tanggal: %s", time.Now().Format("02 January 2006")))
	pdf.Ln(6)
	pdf.Cell(0, 7, fmt.Sprintf("Siswa: %s", student.Name))
	pdf.Ln(10)

	pdf.SetFont("Arial", "B", 11)
	pdf.CellFormat(100, 8, "Paket", "1", 0, "L", false, 0, "")
	pdf.CellFormat(40, 8, "Jumlah", "1", 1, "R", false, 0, "")

	pdf.SetFont("Arial", "", 11)
	pdf.CellFormat(100, 8, pkg.Name, "1", 0, "L", false, 0, "")
	pdf.CellFormat(40, 8, fmt.Sprintf("Rp%.0f", payment.Amount), "1", 1, "R", false, 0, "")

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
