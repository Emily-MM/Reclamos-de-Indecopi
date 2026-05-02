package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	UMBRAL_SPAM      = 5
	UMBRAL_PICO      = 5
	NUM_EJECUCIONES  = 10
	PORCENTAJE_CORTE = 20
)

type Reclamo struct {
	IDReclamo  string
	Timestamp  time.Time
	TsValido   bool
	TsRaw      string
	Materia    string
	Denunciado string
}

type Resultado struct {
	AlertasSpam   int
	AlertasRafaga int
	TotalAlertas  int
}

type ResultadoParcial struct {
	ConteoTexto     map[string]int
	TsPorDenunciado map[string][]time.Time
}

type GrupoTimestamps struct {
	Denunciado string
	Timestamps []time.Time
}

type Lote struct {
	ID     int
	Inicio int
	Fin    int
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
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(record) < 7 {
			continue
		}

		r := Reclamo{
			IDReclamo:  strings.TrimSpace(record[0]),
			TsRaw:      strings.TrimSpace(record[1]),
			Materia:    strings.TrimSpace(record[3]),
			Denunciado: strings.TrimSpace(record[4]),
		}

		if r.TsRaw != "" {
			t, err := time.Parse("02/01/2006 15:04:05", r.TsRaw)
			if err != nil && len(r.TsRaw) >= 10 {
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func procesarLote(filas []Reclamo) ResultadoParcial {
	conteoTexto := make(map[string]int)
	tsPorDenunciado := make(map[string][]time.Time)

	for _, r := range filas {
		if r.Materia != "" && r.Denunciado != "" {
			clave := r.Materia + "|" + r.Denunciado
			conteoTexto[clave]++
		}

		if r.TsValido && r.Denunciado != "" {
			tsPorDenunciado[r.Denunciado] = append(tsPorDenunciado[r.Denunciado], r.Timestamp)
		}
	}

	return ResultadoParcial{
		ConteoTexto:     conteoTexto,
		TsPorDenunciado: tsPorDenunciado,
	}
}

func contarSpam(conteoTexto map[string]int) int {
	alertas := 0
	for _, conteo := range conteoTexto {
		if conteo >= UMBRAL_SPAM {
			alertas++
		}
	}
	return alertas
}

func tieneRafaga(timestamps []time.Time) bool {
	sort.Slice(timestamps, func(i, j int) bool {
		return timestamps[i].Before(timestamps[j])
	})

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

func contarRafagasConcurrente(tsPorDenunciado map[string][]time.Time, numWorkers int) int {
	trabajos := make(chan GrupoTimestamps)
	resultados := make(chan int, numWorkers)

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			alertasLocales := 0
			for grupo := range trabajos {
				if tieneRafaga(grupo.Timestamps) {
					alertasLocales++
				}
			}
			resultados <- alertasLocales
		}()
	}

	go func() {
		for denunciado, timestamps := range tsPorDenunciado {
			trabajos <- GrupoTimestamps{
				Denunciado: denunciado,
				Timestamps: timestamps,
			}
		}
		close(trabajos)
		wg.Wait()
		close(resultados)
	}()

	total := 0
	for parcial := range resultados {
		total += parcial
	}

	return total
}

func detectarConcurrente(filas []Reclamo, numWorkers int) Resultado {
	if numWorkers < 1 {
		numWorkers = 1
	}
	if len(filas) == 0 {
		return Resultado{}
	}
	numWorkers = min(numWorkers, len(filas))

	lotes := make(chan Lote, numWorkers)
	resultados := make(chan ResultadoParcial, numWorkers)
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for lote := range lotes {
				resultados <- procesarLote(filas[lote.Inicio:lote.Fin])
			}
		}()
	}

	go func() {
		tamanoLote := (len(filas) + numWorkers - 1) / numWorkers
		id := 0
		for inicio := 0; inicio < len(filas); inicio += tamanoLote {
			fin := min(inicio+tamanoLote, len(filas))
			lotes <- Lote{
				ID:     id,
				Inicio: inicio,
				Fin:    fin,
			}
			id++
		}
		close(lotes)
	}()

	go func() {
		wg.Wait()
		close(resultados)
	}()

	conteoTextoGlobal := make(map[string]int)
	tsPorDenunciadoGlobal := make(map[string][]time.Time)

	for parcial := range resultados {
		for clave, conteo := range parcial.ConteoTexto {
			conteoTextoGlobal[clave] += conteo
		}
		for denunciado, timestamps := range parcial.TsPorDenunciado {
			tsPorDenunciadoGlobal[denunciado] = append(tsPorDenunciadoGlobal[denunciado], timestamps...)
		}
	}

	alertasSpam := contarSpam(conteoTextoGlobal)
	alertasRafaga := contarRafagasConcurrente(tsPorDenunciadoGlobal, numWorkers)

	return Resultado{
		AlertasSpam:   alertasSpam,
		AlertasRafaga: alertasRafaga,
		TotalAlertas:  alertasSpam + alertasRafaga,
	}
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
	numWorkers := runtime.NumCPU()

	filas, err := leerCSV(inputFile)
	if err != nil {
		fmt.Printf("error leyendo archivo: %v\n", err)
		os.Exit(1)
	}

	conTs := 0
	for _, r := range filas {
		if r.TsValido {
			conTs++
		}
	}
	fmt.Printf("%d filas | %d con timestamp (%.1f%%)\n\n",
		len(filas), conTs, float64(conTs)/float64(len(filas))*100)

	var memAntes runtime.MemStats
	runtime.ReadMemStats(&memAntes)

	tiempos := make([]float64, NUM_EJECUCIONES)
	var ultimoResultado Resultado

	for i := 0; i < NUM_EJECUCIONES; i++ {
		inicio := time.Now()
		ultimoResultado = detectarConcurrente(filas, numWorkers)
		elapsed := time.Since(inicio).Seconds()
		tiempos[i] = elapsed
		fmt.Printf("run %2d: %.4f s | alertas: %d\n", i+1, elapsed, ultimoResultado.TotalAlertas)
	}

	var memDespues runtime.MemStats
	runtime.ReadMemStats(&memDespues)

	media := mediaRecortada(tiempos, PORCENTAJE_CORTE)

	sorted := make([]float64, NUM_EJECUCIONES)
	copy(sorted, tiempos)
	sort.Float64s(sorted)

	fmt.Printf("\nspam:   %d\n", ultimoResultado.AlertasSpam)
	fmt.Printf("rafaga: %d\n", ultimoResultado.AlertasRafaga)
	fmt.Printf("total:  %d\n", ultimoResultado.TotalAlertas)

	fmt.Printf("\nmin:             %.4f s\n", sorted[0])
	fmt.Printf("max:             %.4f s\n", sorted[len(sorted)-1])
	fmt.Printf("media recortada: %.4f s (T-Concurrente)\n", media)

	fmt.Printf("\nheap antes:   %.2f MB\n", float64(memAntes.HeapAlloc)/1024/1024)
	fmt.Printf("heap despues: %.2f MB\n", float64(memDespues.HeapAlloc)/1024/1024)
	fmt.Printf("total aloc:   %.2f MB\n", float64(memDespues.TotalAlloc)/1024/1024)
	fmt.Printf("nucleos:      %d\n", runtime.NumCPU())
}
