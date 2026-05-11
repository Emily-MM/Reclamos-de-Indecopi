package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	UMBRAL_PICO      = 5
	NUM_EJECUCIONES  = 100
	PORCENTAJE_CORTE = 20
)

type Reclamo struct {
	IDReclamo  string
	Timestamp  time.Time
	TsValido   bool
	TsRaw      string
	Materia    string
	Texto      string
	Denunciado string
}

type Clasificacion struct {
	IDReclamo        string
	Clasificacion    string
	SeñalesActivadas []string
	TotalSeñales     int
}

type Resultado struct {
	TotalHumano     int
	TotalSospechoso int
	TotalBot        int
	TotalAlertas    int
}

type Lote struct {
	Inicio int
	Fin    int
}

type IndiceParcial struct {
	ConteoTextos     map[string]int
	TsPorDenunciado map[string][]time.Time
}

type ResultadoParcial struct {
	TotalHumano     int
	TotalSospechoso int
	TotalBot        int
}

type EjecucionLog struct {
	Run             int     `json:"run"`
	TiempoS         float64 `json:"tiempo_s"`
	TotalHumano     int     `json:"total_humano"`
	TotalSospechoso int     `json:"total_sospechoso"`
	TotalBot        int     `json:"total_bot"`
	TotalAlertas    int     `json:"total_alertas"`
	HeapMB          float64 `json:"heap_mb"`
}

type LogFinal struct {
	Version          string         `json:"version"`
	Fecha            string         `json:"fecha"`
	TotalFilas       int            `json:"total_filas"`
	TotalEjecuciones int            `json:"total_ejecuciones"`
	TiempoMinS       float64        `json:"tiempo_min_s"`
	TiempoMaxS       float64        `json:"tiempo_max_s"`
	MediaRecortadaS  float64        `json:"media_recortada_s"`
	HeapAntesM       float64        `json:"heap_antes_mb"`
	HeapDespuesM     float64        `json:"heap_despues_mb"`
	NucleosCPU       int            `json:"nucleos_cpu"`
	Workers          int            `json:"workers"`
	TamañoLote       int            `json:"tamano_lote"`
	Ejecuciones      []EjecucionLog `json:"ejecuciones"`
}

var reFormatoCodigo = regexp.MustCompile(`[A-Z]{2,}_[A-Z]{2,}`)

func leerCSV(ruta string) ([]Reclamo, error) {
	f, err := os.Open(ruta)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1

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
			lineNum++
			continue
		}
		lineNum++
		if len(record) < 9 {
			continue
		}

		r := Reclamo{
			IDReclamo:  strings.TrimSpace(record[0]),
			TsRaw:      strings.TrimSpace(record[1]),
			Materia:    strings.TrimSpace(record[3]),
			Texto:      strings.TrimSpace(record[4]),
			Denunciado: strings.TrimSpace(record[6]),
		}

		if r.TsRaw != "" {
			t, err := time.Parse("02/01/2006 15:04:05", r.TsRaw)
			if err != nil {
				t, err = time.Parse("02/01/2006", r.TsRaw[:10])
			}
			if err == nil {
				r.Timestamp = t
				r.TsValido = true
			}
		}

		filas = append(filas, r)
	}

	return filas, nil
}

func señalTextoIdentico(texto string, conteoTextos map[string]int) bool {
	if texto == "" {
		return false
	}
	return conteoTextos[texto] > 1
}

func señalSoloMayusculas(texto string) bool {
	if texto == "" {
		return false
	}
	for _, r := range texto {
		if unicode.IsLetter(r) && unicode.IsLower(r) {
			return false
		}
	}
	return true
}

func señalSinPuntuacion(texto string) bool {
	if texto == "" {
		return false
	}
	for _, r := range texto {
		if r == '.' || r == ',' || r == '!' || r == '?' || r == ';' || r == ':' {
			return false
		}
	}
	return true
}

func señalCaracteresEspeciales(texto string) bool {
	if texto == "" {
		return false
	}
	total := 0
	especiales := 0
	for _, r := range texto {
		total++
		if !unicode.IsLetter(r) && !unicode.IsSpace(r) {
			especiales++
		}
	}
	if total == 0 {
		return false
	}
	return float64(especiales)/float64(total) > 0.30
}

func señalPalabraLarga(texto string) bool {
	for _, palabra := range strings.Fields(texto) {
		if len([]rune(palabra)) > 20 {
			return true
		}
	}
	return false
}

func señalFormatoCodigo(texto string) bool {
	return reFormatoCodigo.MatchString(texto)
}

func señalRafaga(timestamps []time.Time) bool {
	if len(timestamps) < UMBRAL_PICO {
		return false
	}
	inicio := 0
	for fin := 0; fin < len(timestamps); fin++ {
		for timestamps[fin].Sub(timestamps[inicio]) > time.Hour {
			inicio++
		}
		if fin-inicio+1 >= UMBRAL_PICO {
			return true
		}
	}
	return false
}

func señalRafagaPrecalculada(denunciado string, denunciadosConRafaga map[string]bool) bool {
	if denunciado == "" {
		return false
	}
	return denunciadosConRafaga[denunciado]
}

func clasificar(r Reclamo, conteoTextos map[string]int, denunciadosConRafaga map[string]bool) Clasificacion {
	var señales []string

	if señalTextoIdentico(r.Texto, conteoTextos) {
		señales = append(señales, "texto_identico")
	}
	if señalSoloMayusculas(r.Texto) {
		señales = append(señales, "solo_mayusculas")
	}
	if señalSinPuntuacion(r.Texto) {
		señales = append(señales, "sin_puntuacion")
	}
	if señalCaracteresEspeciales(r.Texto) {
		señales = append(señales, "caracteres_especiales")
	}
	if señalPalabraLarga(r.Texto) {
		señales = append(señales, "palabra_larga")
	}
	if señalFormatoCodigo(r.Texto) {
		señales = append(señales, "formato_codigo")
	}
	if r.TsValido && señalRafagaPrecalculada(r.Denunciado, denunciadosConRafaga) {
		señales = append(señales, "rafaga_tiempo")
	}

	total := len(señales)
	clasificacion := "HUMANO"
	if total >= 4 {
		clasificacion = "BOT"
	} else if total >= 2 {
		clasificacion = "SOSPECHOSO"
	}

	return Clasificacion{
		IDReclamo:        r.IDReclamo,
		Clasificacion:    clasificacion,
		SeñalesActivadas: señales,
		TotalSeñales:     total,
	}
}

func normalizarWorkers(workers int) int {
	if workers < 1 {
		return 1
	}
	return workers
}

func calcularTamañoLote(totalFilas int, workers int) int {
	workers = normalizarWorkers(workers)
	tamaño := totalFilas / (workers * 8)
	if tamaño < 1000 {
		return 1000
	}
	if tamaño > 50000 {
		return 50000
	}
	return tamaño
}

func enviarLotes(totalFilas int, tamañoLote int, lotes chan<- Lote) {
	defer close(lotes)
	for inicio := 0; inicio < totalFilas; inicio += tamañoLote {
		fin := inicio + tamañoLote
		if fin > totalFilas {
			fin = totalFilas
		}
		lotes <- Lote{Inicio: inicio, Fin: fin}
	}
}

func construirIndicesConcurrente(filas []Reclamo, workers int, tamañoLote int) (map[string]int, map[string][]time.Time) {
	workers = normalizarWorkers(workers)
	lotes := make(chan Lote, workers*2)
	parciales := make(chan IndiceParcial, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			conteoTextosLocal := make(map[string]int)
			tsPorDenunciadoLocal := make(map[string][]time.Time)

			for lote := range lotes {
				for j := lote.Inicio; j < lote.Fin; j++ {
					r := filas[j]
					if r.Texto != "" {
						conteoTextosLocal[r.Texto]++
					}
					if r.TsValido && r.Denunciado != "" {
						tsPorDenunciadoLocal[r.Denunciado] = append(tsPorDenunciadoLocal[r.Denunciado], r.Timestamp)
					}
				}
			}

			parciales <- IndiceParcial{
				ConteoTextos:     conteoTextosLocal,
				TsPorDenunciado: tsPorDenunciadoLocal,
			}
		}()
	}

	go enviarLotes(len(filas), tamañoLote, lotes)

	go func() {
		wg.Wait()
		close(parciales)
	}()

	conteoTextos := make(map[string]int)
	tsPorDenunciado := make(map[string][]time.Time)

	for parcial := range parciales {
		for texto, conteo := range parcial.ConteoTextos {
			conteoTextos[texto] += conteo
		}
		for denunciado, timestamps := range parcial.TsPorDenunciado {
			tsPorDenunciado[denunciado] = append(tsPorDenunciado[denunciado], timestamps...)
		}
	}

	return conteoTextos, tsPorDenunciado
}

func calcularRafagasConcurrente(tsPorDenunciado map[string][]time.Time, workers int) map[string]bool {
	workers = normalizarWorkers(workers)
	trabajos := make(chan string, workers*2)
	resultados := make(chan string, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for denunciado := range trabajos {
				timestamps := tsPorDenunciado[denunciado]
				sort.Slice(timestamps, func(i, j int) bool {
					return timestamps[i].Before(timestamps[j])
				})
				if señalRafaga(timestamps) {
					resultados <- denunciado
				}
			}
		}()
	}

	go func() {
		for denunciado := range tsPorDenunciado {
			trabajos <- denunciado
		}
		close(trabajos)
	}()

	go func() {
		wg.Wait()
		close(resultados)
	}()

	denunciadosConRafaga := make(map[string]bool)
	for denunciado := range resultados {
		denunciadosConRafaga[denunciado] = true
	}

	return denunciadosConRafaga
}

func clasificarConcurrente(filas []Reclamo, conteoTextos map[string]int, denunciadosConRafaga map[string]bool, workers int, tamañoLote int) Resultado {
	workers = normalizarWorkers(workers)
	lotes := make(chan Lote, workers*2)
	parciales := make(chan ResultadoParcial, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			parcial := ResultadoParcial{}
			for lote := range lotes {
				for j := lote.Inicio; j < lote.Fin; j++ {
					c := clasificar(filas[j], conteoTextos, denunciadosConRafaga)
					switch c.Clasificacion {
					case "BOT":
						parcial.TotalBot++
					case "SOSPECHOSO":
						parcial.TotalSospechoso++
					default:
						parcial.TotalHumano++
					}
				}
			}
			parciales <- parcial
		}()
	}

	go enviarLotes(len(filas), tamañoLote, lotes)

	go func() {
		wg.Wait()
		close(parciales)
	}()

	resultado := Resultado{}
	for parcial := range parciales {
		resultado.TotalBot += parcial.TotalBot
		resultado.TotalSospechoso += parcial.TotalSospechoso
		resultado.TotalHumano += parcial.TotalHumano
	}

	resultado.TotalAlertas = resultado.TotalBot + resultado.TotalSospechoso
	return resultado
}

func detectarConcurrente(filas []Reclamo, workers int, tamañoLote int) Resultado {
	conteoTextos, tsPorDenunciado := construirIndicesConcurrente(filas, workers, tamañoLote)
	denunciadosConRafaga := calcularRafagasConcurrente(tsPorDenunciado, workers)
	return clasificarConcurrente(filas, conteoTextos, denunciadosConRafaga, workers, tamañoLote)
}

func mediaRecortada(tiempos []float64, pct int) float64 {
	n := len(tiempos)
	sorted := make([]float64, n)
	copy(sorted, tiempos)
	sort.Float64s(sorted)

	corte := int(float64(n) * float64(pct) / 100.0)
	recortados := sorted[corte : n-corte]

	suma := 0.0
	for _, t := range recortados {
		suma += t
	}
	return suma / float64(len(recortados))
}

func main() {
	inputFile := "dataset_indecopi_limpio.csv"
	logFile := "logs_concurrente.json"
	workers := runtime.NumCPU()

	filas, err := leerCSV(inputFile)
	if err != nil {
		fmt.Printf("error leyendo archivo: %v\n", err)
		os.Exit(1)
	}

	tamañoLote := calcularTamañoLote(len(filas), workers)

	conTs := 0
	for _, r := range filas {
		if r.TsValido {
			conTs++
		}
	}
	fmt.Printf("%d filas | %d con timestamp (%.1f%%)\n",
		len(filas), conTs, float64(conTs)/float64(len(filas))*100)
	fmt.Printf("workers: %d | tamaño lote: %d\n\n", workers, tamañoLote)

	var memAntes runtime.MemStats
	runtime.ReadMemStats(&memAntes)

	tiempos := make([]float64, NUM_EJECUCIONES)
	logs := make([]EjecucionLog, NUM_EJECUCIONES)
	var ultimoResultado Resultado

	for i := 0; i < NUM_EJECUCIONES; i++ {
		var memRun runtime.MemStats
		runtime.ReadMemStats(&memRun)

		inicio := time.Now()
		ultimoResultado = detectarConcurrente(filas, workers, tamañoLote)
		elapsed := time.Since(inicio).Seconds()
		tiempos[i] = elapsed

		var memPost runtime.MemStats
		runtime.ReadMemStats(&memPost)

		logs[i] = EjecucionLog{
			Run:             i + 1,
			TiempoS:         elapsed,
			TotalHumano:     ultimoResultado.TotalHumano,
			TotalSospechoso: ultimoResultado.TotalSospechoso,
			TotalBot:        ultimoResultado.TotalBot,
			TotalAlertas:    ultimoResultado.TotalAlertas,
			HeapMB:          float64(memPost.HeapAlloc) / 1024 / 1024,
		}

		fmt.Printf("run %3d: %.4f s | bot: %d | sospechoso: %d | humano: %d\n",
			i+1, elapsed, ultimoResultado.TotalBot,
			ultimoResultado.TotalSospechoso, ultimoResultado.TotalHumano)
	}

	var memDespues runtime.MemStats
	runtime.ReadMemStats(&memDespues)

	media := mediaRecortada(tiempos, PORCENTAJE_CORTE)

	sorted := make([]float64, NUM_EJECUCIONES)
	copy(sorted, tiempos)
	sort.Float64s(sorted)

	logData := LogFinal{
		Version:          "concurrente",
		Fecha:            time.Now().Format("02/01/2006 15:04:05"),
		TotalFilas:       len(filas),
		TotalEjecuciones: NUM_EJECUCIONES,
		TiempoMinS:       sorted[0],
		TiempoMaxS:       sorted[len(sorted)-1],
		MediaRecortadaS:  media,
		HeapAntesM:       float64(memAntes.HeapAlloc) / 1024 / 1024,
		HeapDespuesM:     float64(memDespues.HeapAlloc) / 1024 / 1024,
		NucleosCPU:       runtime.NumCPU(),
		Workers:          workers,
		TamañoLote:       tamañoLote,
		Ejecuciones:      logs,
	}

	jsonBytes, err := json.MarshalIndent(logData, "", "  ")
	if err != nil {
		fmt.Printf("error generando JSON: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(logFile, jsonBytes, 0644); err != nil {
		fmt.Printf("error escribiendo JSON: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nbot:        %d\n", ultimoResultado.TotalBot)
	fmt.Printf("sospechoso: %d\n", ultimoResultado.TotalSospechoso)
	fmt.Printf("humano:     %d\n", ultimoResultado.TotalHumano)
	fmt.Printf("alertas:    %d\n", ultimoResultado.TotalAlertas)

	fmt.Printf("\nmin:             %.4f s\n", sorted[0])
	fmt.Printf("max:             %.4f s\n", sorted[len(sorted)-1])
	fmt.Printf("media recortada: %.4f s (T-Concurrente)\n", media)

	fmt.Printf("\nheap antes:   %.2f MB\n", float64(memAntes.HeapAlloc)/1024/1024)
	fmt.Printf("heap despues: %.2f MB\n", float64(memDespues.HeapAlloc)/1024/1024)
	fmt.Printf("nucleos:      %d\n", runtime.NumCPU())
	fmt.Printf("workers:      %d\n", workers)
	fmt.Printf("tamaño lote:  %d\n", tamañoLote)
	fmt.Printf("\nlogs guardados en: %s\n", logFile)
}
