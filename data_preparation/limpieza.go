package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	NUM_WORKERS = 4
	BATCH_SIZE  = 1000
)

type Reclamo struct {
	IDReclamo      string
	Timestamp      string
	TipoExpediente string
	Materia        string
	Denunciado     string
	Canal          string
	Region         string
}

var denunciadoCorrections = map[string]string{
	"BANCO DE CREDITO DEL PERU":  "BANCO DE CRÉDITO DEL PERÚ",
	"BANCO DE CREDITO":           "BANCO DE CRÉDITO DEL PERÚ",
	"CLINICA ANGLO AMERICANA":    "CLÍNICA ANGLO AMERICANA",
	"SAGA FALABELLA S.A":         "SAGA FALABELLA S.A.",
	"RIPLEY CORP S.A":            "RIPLEY CORP S.A.",
	"FINANCIERA OH S.A":          "FINANCIERA OH S.A.",
	"SCOTIABANK PERU S.A.A":      "SCOTIABANK PERÚ S.A.A.",
	"BBVA CONTINENTAL":           "BBVA PERÚ",
	"BBVA BANCO CONTINENTAL":     "BBVA PERÚ",
	"TELEFONICA DEL PERU S.A.A.": "TELEFÓNICA DEL PERÚ S.A.A.",
	"TELEFONICA DEL PERU":        "TELEFÓNICA DEL PERÚ S.A.A.",
	"CLARO PERU S.A.C":           "CLARO PERÚ S.A.C.",
	"AMERICA MOVIL PERU S.A.C.":  "CLARO PERÚ S.A.C.",
}

var (
	totalProcesadas   int
	totalDescartadas  int
	totalTsCorregidos int
	mu                sync.Mutex
)

func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		runes := []rune(w)
		if len(runes) == 0 {
			continue
		}
		runes[0] = unicode.ToUpper(runes[0])
		for j := 1; j < len(runes); j++ {
			runes[j] = unicode.ToLower(runes[j])
		}
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}

func normalizarTimestamp(ts string) (string, bool) {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return "", false
	}

	if len(ts) >= 10 && ts[2] == '/' && ts[5] == '/' {
		return ts, false
	}

	if len(ts) >= 10 && ts[4] == '-' && ts[7] == '-' {
		t, err := time.Parse("2006-01-02 15:04:05", ts)
		if err != nil {
			t, err = time.Parse("2006-01-02", ts[:10])
		}
		if err == nil {
			return t.Format("02/01/2006 15:04:05"), true
		}
	}

	return ts, false
}

func limpiarLote(lote []Reclamo) ([]Reclamo, int, int) {
	resultado := make([]Reclamo, 0, len(lote))
	descartadas := 0
	tsCorregidos := 0

	vistosID := make(map[string]bool)

	for _, r := range lote {

		r.IDReclamo = strings.TrimSpace(r.IDReclamo)
		r.Timestamp = strings.TrimSpace(r.Timestamp)
		r.TipoExpediente = strings.TrimSpace(r.TipoExpediente)
		r.Materia = strings.TrimSpace(r.Materia)
		r.Denunciado = strings.TrimSpace(r.Denunciado)
		r.Canal = strings.TrimSpace(r.Canal)
		r.Region = strings.TrimSpace(r.Region)

		if r.IDReclamo == "" {
			descartadas++
			continue
		}

		if vistosID[r.IDReclamo] {
			descartadas++
			continue
		}
		vistosID[r.IDReclamo] = true

		tsNorm, corregido := normalizarTimestamp(r.Timestamp)
		r.Timestamp = tsNorm
		if corregido {
			tsCorregidos++
		}

		r.TipoExpediente = strings.ToUpper(r.TipoExpediente)
		r.Materia = strings.ToUpper(r.Materia)

		denunciadoKey := strings.ToUpper(r.Denunciado)
		if correccion, existe := denunciadoCorrections[denunciadoKey]; existe {
			r.Denunciado = correccion
		} else {
			r.Denunciado = titleCase(r.Denunciado)
		}

		resultado = append(resultado, r)
	}

	return resultado, descartadas, tsCorregidos
}

func worker(id int, trabajos chan []Reclamo, resultados chan []Reclamo, wg *sync.WaitGroup) {
	defer wg.Done()

	for lote := range trabajos {
		loteLimpio, descartadas, tsCorr := limpiarLote(lote)

		mu.Lock()
		totalProcesadas += len(lote)
		totalDescartadas += descartadas
		totalTsCorregidos += tsCorr
		mu.Unlock()

		resultados <- loteLimpio

		fmt.Printf("  Worker %d → %d filas | válidas: %d | descartadas: %d | ts corregidos: %d | ts vaciados: %d\n",
			id, len(lote), len(loteLimpio), descartadas, tsCorr)
	}
}

func leerCSV(ruta string) ([]Reclamo, error) {
	f, err := os.Open(ruta)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.LazyQuotes = true

	if _, err := reader.Read(); err != nil {
		return nil, err
	}

	var filas []Reclamo
	lineNum := 1
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Printf("  Advertencia línea %d: %v — saltando\n", lineNum, err)
			lineNum++
			continue
		}
		lineNum++
		if len(record) < 7 {
			continue
		}

		filas = append(filas, Reclamo{
			IDReclamo:      record[0],
			Timestamp:      record[1],
			TipoExpediente: record[2],
			Materia:        record[3],
			Denunciado:     record[4],
			Canal:          record[5],
			Region:         record[6],
		})
	}

	return filas, nil
}

func escribirCSV(ruta string, filas []Reclamo) error {
	f, err := os.Create(ruta)
	if err != nil {
		return err
	}
	defer f.Close()

	writer := csv.NewWriter(f)
	defer writer.Flush()

	headers := []string{
		"id_reclamo", "timestamp", "tipo_expediente",
		"materia", "denunciado", "canal", "region",
	}
	if err := writer.Write(headers); err != nil {
		return err
	}

	for _, r := range filas {
		record := []string{
			r.IDReclamo, r.Timestamp, r.TipoExpediente,
			r.Materia, r.Denunciado, r.Canal, r.Region,
		}
		if err := writer.Write(record); err != nil {
			return err
		}
	}

	return nil
}
func main() {
	inputFile := "dataset_indecopi_raw_+1M.csv"
	outputFile := "dataset_indecopi_limpio.csv"

	fmt.Printf("workers: %d | lote: %d filas\n\n", NUM_WORKERS, BATCH_SIZE)

	filas, err := leerCSV(inputFile)
	if err != nil {
		fmt.Printf("error leyendo archivo: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%d filas cargadas\n", len(filas))

	trabajos := make(chan []Reclamo, NUM_WORKERS)
	resultados := make(chan []Reclamo, NUM_WORKERS)

	var wg sync.WaitGroup
	for i := 1; i <= NUM_WORKERS; i++ {
		wg.Add(1)
		go worker(i, trabajos, resultados, &wg)
	}

	go func() {
		for i := 0; i < len(filas); i += BATCH_SIZE {
			fin := i + BATCH_SIZE
			if fin > len(filas) {
				fin = len(filas)
			}
			lote := make([]Reclamo, fin-i)
			copy(lote, filas[i:fin])
			trabajos <- lote
		}
		close(trabajos)
	}()

	var filasLimpias []Reclamo
	var collectWg sync.WaitGroup
	collectWg.Add(1)
	go func() {
		defer collectWg.Done()
		for loteLimpio := range resultados {
			filasLimpias = append(filasLimpias, loteLimpio...)
		}
	}()

	wg.Wait()
	close(resultados)
	collectWg.Wait()

	if err := escribirCSV(outputFile, filasLimpias); err != nil {
		fmt.Printf("error escribiendo archivo: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nleidas:       %d\n", len(filas))
	fmt.Printf("procesadas:   %d\n", totalProcesadas)
	fmt.Printf("descartadas:  %d\n", totalDescartadas)
	fmt.Printf("ts corregidos:%d\n", totalTsCorregidos)
	fmt.Printf("limpio:       %d filas → %s\n", len(filasLimpias), outputFile)
}
