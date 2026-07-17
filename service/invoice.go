// service/invoice.go
package service

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"

	"chronosphere/domain"

	"github.com/go-pdf/fpdf"
)

// formatRupiah formats a float amount into Indonesian thousand-separator
// style, e.g. 1200000 -> "Rp1.200.000"
func formatRupiah(amount float64) string {
	n := int64(amount)
	s := strconv.FormatInt(n, 10)

	neg := false
	if n < 0 {
		neg = true
		s = s[1:]
	}

	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	result := strings.Join(parts, ".")

	if neg {
		result = "-" + result
	}
	return "Rp" + result
}

func GenerateInvoicePDF(payment *domain.Payment, student *domain.User, pkg *domain.Package) ([]byte, error) {
	const (
		companyName = "MDX Music Course"
		companyAddr = "Bali, Indonesia"
	)

	purpleFill := [3]int{230, 220, 245} // header/section band
	purpleLine := [3]int{190, 170, 220}
	grayText := [3]int{90, 90, 90}

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetMargins(15, 15, 15)

	// ── Header: Invoice title + company info ──────────────────────────
	pdf.SetFont("Arial", "B", 26)
	pdf.CellFormat(90, 12, "Invoice", "", 0, "L", false, 0, "")

	pdf.SetFont("Arial", "", 10)
	pdf.SetTextColor(grayText[0], grayText[1], grayText[2])
	pdf.CellFormat(0, 5, companyName, "", 2, "R", false, 0, "")
	pdf.SetX(-95)
	pdf.CellFormat(0, 5, companyAddr, "", 1, "R", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.Ln(6)

	// ── Invoice No / Date ──────────────────────────────────────────────
	pdf.SetFont("Arial", "", 11)
	pdf.CellFormat(95, 7, fmt.Sprintf("No. Invoice: %s", payment.ExternalID), "", 0, "L", false, 0, "")
	pdf.CellFormat(0, 7, fmt.Sprintf("Tanggal: %s", time.Now().Format("02 January 2006")), "", 1, "L", false, 0, "")
	pdf.Ln(4)

	// ── Billing Information band ───────────────────────────────────────
	pdf.SetFillColor(purpleFill[0], purpleFill[1], purpleFill[2])
	pdf.SetFont("Arial", "B", 11)
	pdf.CellFormat(0, 9, "  INFORMASI PENAGIHAN", "", 1, "L", true, 0, "")
	pdf.Ln(3)

	pdf.SetFont("Arial", "", 11)
	pdf.CellFormat(95, 7, fmt.Sprintf("Nama: %s", student.Name), "", 0, "L", false, 0, "")
	pdf.CellFormat(0, 7, fmt.Sprintf("Email: %s", student.Email), "", 1, "L", false, 0, "")
	pdf.CellFormat(95, 7, fmt.Sprintf("Telepon: %s", student.Phone), "", 0, "L", false, 0, "")
	pdf.Ln(10)

	// ── Table header ────────────────────────────────────────────────────
	colDesc, colQty, colCost, colAmt := 90.0, 25.0, 35.0, 30.0
	pdf.SetFillColor(purpleFill[0], purpleFill[1], purpleFill[2])
	pdf.SetFont("Arial", "B", 10)
	pdf.CellFormat(colDesc, 9, "DESKRIPSI", "1", 0, "L", true, 0, "")
	pdf.CellFormat(colQty, 9, "JUMLAH", "1", 0, "C", true, 0, "")
	pdf.CellFormat(colCost, 9, "HARGA", "1", 0, "R", true, 0, "")
	pdf.CellFormat(colAmt, 9, "TOTAL", "1", 1, "R", true, 0, "")

	// ── Table row ────────────────────────────────────────────────────────
	pdf.SetFont("Arial", "", 10)
	pdf.CellFormat(colDesc, 9, pkg.Name, "1", 0, "L", false, 0, "")
	pdf.CellFormat(colQty, 9, "1", "1", 0, "C", false, 0, "")
	pdf.CellFormat(colCost, 9, formatRupiah(pkg.Price), "1", 0, "R", false, 0, "")
	pdf.CellFormat(colAmt, 9, formatRupiah(pkg.Price), "1", 1, "R", false, 0, "")
	pdf.Ln(6)

	// ── Notes box (left) + Totals (right) ────────────────────────────────
	yStart := pdf.GetY()

	pdf.SetFillColor(purpleFill[0], purpleFill[1], purpleFill[2])
	pdf.Rect(15, yStart, 95, 35, "F")
	pdf.SetXY(18, yStart+3)
	pdf.SetFont("Arial", "B", 10)
	pdf.CellFormat(0, 6, "Catatan:", "", 1, "L", false, 0, "")
	pdf.SetX(18)
	pdf.SetFont("Arial", "", 9)
	pdf.MultiCell(88, 5, "Terima kasih telah mendaftar bersama kami. Pembayaran telah dikonfirmasi.", "", "L", false)

	labelX, valueX := 115.0, 155.0
	labelW, valueW := 40.0, 40.0

	discount := pkg.Price - payment.Amount
	if discount < 0 {
		discount = 0
	}

	rows := []struct {
		label string
		value string
	}{
		{"SUBTOTAL", formatRupiah(pkg.Price)},
		{"DISKON", "-" + formatRupiah(discount)},
		{"PAJAK", formatRupiah(0)},
	}
	pdf.SetFont("Arial", "", 10)
	for i, row := range rows {
		y := yStart + float64(i)*7
		pdf.SetXY(labelX, y)
		pdf.CellFormat(labelW, 7, row.label, "", 0, "R", false, 0, "")
		pdf.SetXY(valueX, y)
		pdf.CellFormat(valueW, 7, row.value, "1", 0, "R", false, 0, "")
	}

	totalY := yStart + float64(len(rows))*7
	pdf.SetFillColor(purpleLine[0], purpleLine[1], purpleLine[2])
	pdf.SetFont("Arial", "B", 10)
	pdf.SetXY(labelX, totalY)
	pdf.CellFormat(labelW, 8, "TOTAL", "", 0, "R", false, 0, "")
	pdf.SetXY(valueX, totalY)
	pdf.CellFormat(valueW, 8, formatRupiah(payment.Amount), "1", 0, "R", true, 0, "")
	// ── Footer ──────────────────────────────────────────────────────────
	pdf.SetY(-30)
	pdf.SetFont("Arial", "", 9)
	pdf.SetTextColor(grayText[0], grayText[1], grayText[2])
	pdf.CellFormat(0, 5, fmt.Sprintf("Contact: %s", "admin@mdxmusiccourse.cloud"), "", 1, "L", false, 0, "")

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
