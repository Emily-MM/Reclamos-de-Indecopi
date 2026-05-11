package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"regexp"
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
	Texto          string
	CodReferencia  string
	Denunciado     string
	Canal          string
	Region         string
}

var stopwords = map[string]bool{
	"de": true, "el": true, "la": true, "los": true, "las": true,
	"un": true, "una": true, "y": true, "en": true, "con": true,
	"por": true, "para": true, "que": true, "es": true, "se": true,
	"no": true, "a": true,
}

var denunciadoCorrections = map[string]string{
	"BANCO DE CREDITO DEL PERU":      "BANCO DE CRÉDITO DEL PERÚ",
	"BANCO DE CREDITO DEL PERU S.A.": "BANCO DE CRÉDITO DEL PERÚ",
	"BANCO DE CREDITO":               "BANCO DE CRÉDITO DEL PERÚ",
	"CLINICA ANGLO AMERICANA":        "CLÍNICA ANGLO AMERICANA",
	"SAGA FALABELLA S.A":             "SAGA FALABELLA S.A.",
	"SAGA FALABELLA S A":             "SAGA FALABELLA S.A.",
	"RIPLEY CORP S.A":                "RIPLEY CORP S.A.",
	"FINANCIERA OH S.A":              "FINANCIERA OH S.A.",
	"SCOTIABANK PERU S.A.A":          "SCOTIABANK PERÚ S.A.A.",
	"SCOTIABANK PERU SAA":            "SCOTIABANK PERÚ S.A.A.",
	"BBVA CONTINENTAL":               "BBVA PERÚ",
	"BBVA BANCO CONTINENTAL":         "BBVA PERÚ",
	"BBVA BANCO CONTINENTAL S.A.":    "BBVA PERÚ",
	"TELEFONICA DEL PERU S.A.A.":     "TELEFÓNICA DEL PERÚ S.A.A.",
	"TELEFONICA DEL PERU":            "TELEFÓNICA DEL PERÚ S.A.A.",
	"CLARO PERU S.A.C":               "CLARO PERÚ S.A.C.",
	"AMERICA MOVIL PERU S.A.C.":      "CLARO PERÚ S.A.C.",
}

var (
	totalProcesadas   int
	totalDescartadas  int
	totalTsCorregidos int
	totalFilasUnidas  int
	mu                sync.Mutex
)

var reRef = regexp.MustCompile(`\[REF-(\d+)\]`)

var prefijosOllama = []string{
	"Aquí te dejo mi reclamo:",
	"Aquí te dejo el reclamo:",
	"Aquí te dejo el cuerpo del reclamo:",
	"Aquí está el reclamo:",
	"Aquí está el cuerpo del reclamo:",
	"Aquí va el reclamo:",
	"Aquí va un reclamo de 20 a 50 palabras:",
	"Aquí va el cuerpo del reclamo:",
	"Espero que te guste.",
}

var reMetadatos = regexp.MustCompile(
	`(?i)(Sector General|Materia Específica|Materia Especifica|Tipo de Expediente)\s*:\s*[^\n]+\n?`)
var reIndicadorPalabras = regexp.MustCompile(`\(\d+\s*palabras?\)`)

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

func limpiarTexto(texto string) (string, string) {
	texto = strings.TrimSpace(texto)

	for _, prefijo := range prefijosOllama {
		if idx := strings.Index(strings.ToLower(texto), strings.ToLower(prefijo)); idx != -1 {
			texto = texto[idx+len(prefijo):]
		}
	}

	texto = reMetadatos.ReplaceAllString(texto, "")
	texto = reIndicadorPalabras.ReplaceAllString(texto, "")

	lineas := strings.Split(texto, "\n")
	limpias := make([]string, 0, len(lineas))
	for _, l := range lineas {
		l = strings.TrimSpace(l)
		if l != "" {
			limpias = append(limpias, l)
		}
	}
	texto = strings.Join(limpias, " ")

	codRef := ""
	match := reRef.FindStringSubmatch(texto)
	if len(match) > 1 {
		codRef = match[1]
	}
	texto = reRef.ReplaceAllString(texto, "")

	palabras := strings.Fields(texto)
	resultado := make([]string, 0, len(palabras))
	for _, p := range palabras {
		pLower := strings.ToLower(p)
		pClean := strings.TrimFunc(pLower, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		})
		if !stopwords[pClean] {
			resultado = append(resultado, p)
		}
	}

	return strings.TrimSpace(strings.Join(resultado, " ")), codRef
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
		r.Texto = strings.TrimSpace(r.Texto)
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
		r.Canal = strings.ToUpper(strings.TrimSpace(r.Canal))
		r.Region = strings.ToUpper(strings.TrimSpace(r.Region))

		r.Texto, r.CodReferencia = limpiarTexto(r.Texto)

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

		fmt.Printf("worker %d → %d filas | válidas: %d | descartadas: %d | ts corregidos: %d\n",
			id, len(lote), len(loteLimpio), descartadas, tsCorr)
	}
}

func leerCSV(ruta string) ([]Reclamo, int, error) {
	f, err := os.Open(ruta)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1

	if _, err := reader.Read(); err != nil {
		return nil, 0, err
	}

	var filas []Reclamo
	filasUnidas := 0
	lineNum := 1

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Printf("advertencia línea %d: %v — saltando\n", lineNum, err)
			lineNum++
			continue
		}
		lineNum++

		if len(filas) > 0 && (len(record) < 8 || strings.TrimSpace(record[0]) == "") {
			continuacion := strings.TrimSpace(strings.Join(record, " "))
			if continuacion != "" {
				filas[len(filas)-1].Texto += " " + continuacion
				filasUnidas++
			}
			continue
		}

		if len(record) < 8 {
			continue
		}

		filas = append(filas, Reclamo{
			IDReclamo:      record[0],
			Timestamp:      record[1],
			TipoExpediente: record[2],
			Materia:        record[3],
			Texto:          record[4],
			Denunciado:     record[5],
			Canal:          record[6],
			Region:         record[7],
		})
	}

	return filas, filasUnidas, nil
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
		"id_reclamo", "timestamp", "tipo_expediente", "materia",
		"texto", "cod_referencia", "denunciado", "canal", "region",
	}
	if err := writer.Write(headers); err != nil {
		return err
	}

	for _, r := range filas {
		record := []string{
			r.IDReclamo, r.Timestamp, r.TipoExpediente, r.Materia,
			r.Texto, r.CodReferencia, r.Denunciado, r.Canal, r.Region,
		}
		if err := writer.Write(record); err != nil {
			return err
		}
	}

	return nil
}

func main() {
	inputFile := "dataset_indecopi_texto_v2.csv"
	outputFile := "dataset_indecopi_limpio.csv"

	fmt.Printf("workers: %d | lote: %d filas\n\n", NUM_WORKERS, BATCH_SIZE)

	filas, filasUnidas, err := leerCSV(inputFile)
	if err != nil {
		fmt.Printf("error leyendo archivo: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%d filas cargadas | %d filas partidas unidas\n", len(filas), filasUnidas)

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

	fmt.Printf("\nleidas:        %d\n", len(filas))
	fmt.Printf("filas unidas:  %d\n", filasUnidas)
	fmt.Printf("procesadas:    %d\n", totalProcesadas)
	fmt.Printf("descartadas:   %d\n", totalDescartadas)
	fmt.Printf("ts corregidos: %d\n", totalTsCorregidos)
	fmt.Printf("limpio:        %d filas → %s\n", len(filasLimpias), outputFile)
}
