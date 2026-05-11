# Informe Técnico de GAPs

## Sistema de detección de patrones sospechosos en reclamos ciudadanos – INDECOPI

Repositorio analizado: urlReclamos-de-Indecopi[https://github.com/Emily-MM/Reclamos-de-Indecopi](https://github.com/Emily-MM/Reclamos-de-Indecopi)

## Resumen Ejecutivo

El proyecto implementa un pipeline de procesamiento de reclamos ciudadanos utilizando Go y técnicas de concurrencia mediante goroutines, channels y worker pools. El sistema incluye:

- Limpieza concurrente de más de 1.2 millones de registros.
- Detector secuencial basado en reglas heurísticas.
- Detector concurrente con pipeline paralelizado.
- Uso de estructuras compartidas y procesamiento por lotes.

La arquitectura general es funcional y demuestra conocimiento intermedio de programación concurrente en Go. Sin embargo, existen múltiples GAPs relacionados con robustez, mantenibilidad, validación de datos, control de errores, eficiencia de memoria y patrones concurrentes.

---

# 1. CALIDAD DE CÓDIGO

## 1.1 Duplicación de código entre versiones secuencial y concurrente

| Aspecto   | Detalle                                                                                                  |
| --------- | -------------------------------------------------------------------------------------------------------- |
| Severidad | Media                                                                                                    |
| Problema  | Existe una gran cantidad de lógica duplicada entre `detector_secuencial.go` y `detector_conccurente.go`. |
| Impacto   | Incrementa el costo de mantenimiento y el riesgo de inconsistencias.                                     |

### Fragmento afectado

```go
func señalSoloMayusculas(texto string) bool
func señalSinPuntuacion(texto string) bool
func señalCaracteresEspeciales(texto string) bool
func señalPalabraLarga(texto string) bool
```

### Recomendación

Extraer la lógica compartida hacia:

- un paquete `detector/`
- un paquete `rules/`
- utilidades comunes reutilizables

Aplicar separación por capas:

```text
/cmd
/internal/detector
/internal/pipeline
/internal/cleaner
/internal/model
/internal/utils
```

---

## 1.2 Ausencia de manejo explícito de errores

| Aspecto   | Detalle                                                            |
| --------- | ------------------------------------------------------------------ |
| Severidad | Alta                                                               |
| Problema  | Muchos errores son ignorados silenciosamente.                      |
| Impacto   | Riesgo de corrupción silenciosa de datos y diagnósticos difíciles. |

### Fragmento afectado

```go
record, err := reader.Read()
if err != nil {
    lineNum++
    continue
}
```

### Problemas detectados

- No se registra el error.
- No se contabilizan filas corruptas.
- Se pierde trazabilidad.
- El procesamiento continúa aunque exista corrupción masiva.

### Recomendación

Implementar logging estructurado:

```go
log.Printf("error leyendo línea %d: %v", lineNum, err)
```

Agregar métricas:

- filas inválidas
- timestamps corruptos
- registros descartados
- errores por lote

---

## 1.3 Variables y funciones sin documentación

| Aspecto   | Detalle                                                        |
| --------- | -------------------------------------------------------------- |
| Severidad | Media                                                          |
| Problema  | No existen comentarios GoDoc ni documentación técnica interna. |

### Fragmento afectado

```go
func señalRafaga(timestamps []time.Time) bool
```

### Recomendación

Aplicar comentarios GoDoc:

```go
// señalRafaga detecta múltiples reclamos en una ventana de una hora.
func señalRafaga(timestamps []time.Time) bool
```

---

## 1.4 Uso inconsistente de nombres y convenciones

| Aspecto   | Detalle                                                                    |
| --------- | -------------------------------------------------------------------------- |
| Severidad | Baja                                                                       |
| Problema  | Se mezclan nombres en español con estructuras de estilo Go no idiomáticas. |

### Problemas observados

- `NUM_WORKERS`
- `BATCH_SIZE`
- `PORCENTAJE_CORTE`
- mezcla de camelCase y MAYÚSCULAS

### Recomendación

Seguir Effective Go:

```go
const numWorkers = 4
const batchSize = 1000
```

---

## 1.5 Variables no utilizadas

| Aspecto   | Detalle                                                   |
| --------- | --------------------------------------------------------- |
| Severidad | Baja                                                      |
| Problema  | Algunas variables como `lineNum` no tienen utilidad real. |

### Recomendación

Eliminar variables innecesarias o utilizarlas para auditoría y logging.

---

# 2. SEGURIDAD

## 2.1 Validación insuficiente de entradas CSV

| Aspecto   | Detalle                                                                 |
| --------- | ----------------------------------------------------------------------- |
| Severidad | Alta                                                                    |
| Problema  | El sistema asume formatos válidos de fecha y longitud mínima de campos. |

### Fragmento afectado

```go
t, err = time.Parse("02/01/2006", r.TsRaw[:10])
```

### Riesgo

Puede provocar:

- panic por slicing fuera de rango
- corrupción de datos
- caídas del proceso

### Recomendación

Validar longitud antes de acceder:

```go
if len(r.TsRaw) >= 10 {
    t, err = time.Parse("02/01/2006", r.TsRaw[:10])
}
```

---

## 2.2 Ausencia de límites de memoria o tamaño de input

| Aspecto   | Detalle                                        |
| --------- | ---------------------------------------------- |
| Severidad | Alta                                           |
| Problema  | Todo el CSV se carga completamente en memoria. |

### Fragmento afectado

```go
var filas []Reclamo
filas = append(filas, r)
```

### Riesgo

Un archivo extremadamente grande puede generar:

- consumo excesivo de RAM
- OOM (Out Of Memory)
- degradación del sistema

### Recomendación

Implementar procesamiento streaming:

```go
reader.Read()
-> channel
-> workers
-> procesamiento incremental
```

---

## 2.3 Datos sensibles expuestos en logs

| Aspecto   | Detalle                                                         |
| --------- | --------------------------------------------------------------- |
| Severidad | Media                                                           |
| Problema  | El sistema imprime información operativa directamente a stdout. |

### Fragmento afectado

```go
fmt.Printf("worker %d → %d filas", ...)
```

### Riesgo

En entornos reales podría exponerse:

- IDs internos
- patrones de denuncias
- información operacional

### Recomendación

Usar logging configurable:

```go
logrus
zap
zerolog
```

Implementar niveles:

- INFO
- WARN
- ERROR
- DEBUG

---

## 2.4 Expresiones regulares potencialmente costosas

| Aspecto   | Detalle                                                                  |
| --------- | ------------------------------------------------------------------------ |
| Severidad | Baja                                                                     |
| Problema  | Algunas regex se aplican repetidamente sobre grandes volúmenes de texto. |

### Fragmento afectado

```go
reMetadatos.ReplaceAllString(texto, "")
```

### Recomendación

Evaluar:

- compilación única (ya implementada parcialmente)
- reducción de complejidad regex
- procesamiento basado en strings cuando sea posible

---

# 3. PATRONES DE CONCURRENCIA

## 3.1 Uso global de Mutex para contadores compartidos

| Aspecto   | Detalle                                                 |
| --------- | ------------------------------------------------------- |
| Severidad | Media                                                   |
| Problema  | El mutex global puede convertirse en cuello de botella. |

### Fragmento afectado

```go
mu.Lock()
totalProcesadas += len(lote)
totalDescartadas += descartadas
mu.Unlock()
```

### Impacto

- serialización innecesaria
- menor escalabilidad
- contención entre workers

### Recomendación

Usar:

```go
atomic.AddInt64()
```

O consolidación por reducción al final.

---

## 3.2 Posible fuga de goroutines ante bloqueo de channels

| Aspecto   | Detalle                                                                       |
| --------- | ----------------------------------------------------------------------------- |
| Severidad | Alta                                                                          |
| Problema  | Algunos pipelines dependen de consumo correcto sin mecanismos de cancelación. |

### Fragmento afectado

```go
resultados <- loteLimpio
```

### Riesgo

Si el consumidor deja de leer:

- workers quedan bloqueados
- goroutines quedan colgadas
- fuga de recursos

### Recomendación

Incorporar:

```go
context.Context
select {
case resultados <- dato:
case <-ctx.Done():
}
```

---

## 3.3 Ausencia de context cancellation

| Aspecto   | Detalle                                                   |
| --------- | --------------------------------------------------------- |
| Severidad | Alta                                                      |
| Problema  | El sistema no permite cancelar procesamiento concurrente. |

### Impacto

- imposibilidad de abortar pipelines
- mala resiliencia operacional
- workers activos incluso ante error crítico

### Recomendación

Integrar:

```go
context.WithCancel()
```

Y propagar contexto a:

- workers
- pipelines
- lectura CSV
- clasificación

---

## 3.4 Riesgo de race conditions futuras

| Aspecto   | Detalle                                                         |
| --------- | --------------------------------------------------------------- |
| Severidad | Media                                                           |
| Problema  | Se comparten mapas entre goroutines sin encapsulación estricta. |

### Fragmento afectado

```go
map[string][]time.Time
map[string]int
```

### Observación

Actualmente el acceso parece controlado, pero futuras modificaciones pueden introducir race conditions fácilmente.

### Recomendación

- encapsular acceso
- documentar ownership de datos
- ejecutar siempre:

```bash
go test -race
```

---

## 3.5 Uso limitado del modelo pipeline

| Aspecto   | Detalle                                                                        |
| --------- | ------------------------------------------------------------------------------ |
| Severidad | Media                                                                          |
| Problema  | El pipeline concurrente no implementa backpressure real ni control adaptativo. |

### Recomendación

Aplicar:

- bounded workers
- dynamic batching
- worker scaling
- context propagation
- retry control

---

## 3.6 Riesgo de deadlock por dependencia de cierre manual

| Aspecto   | Detalle                                                                        |
| --------- | ------------------------------------------------------------------------------ |
| Severidad | Media                                                                          |
| Problema  | El cierre correcto depende de coordinación manual entre WaitGroups y channels. |

### Fragmento afectado

```go
wg.Wait()
close(parciales)
```

### Riesgo

Cambios futuros pueden introducir:

- sends sobre channel cerrado
- goroutines bloqueadas
- deadlocks difíciles de depurar

### Recomendación

Encapsular pipelines en estructuras reutilizables.

Agregar tests concurrentes automatizados.

---

# 4. RENDIMIENTO

## 4.1 Carga completa del dataset en memoria

| Aspecto   | Detalle                                                 |
| --------- | ------------------------------------------------------- |
| Severidad | Alta                                                    |
| Problema  | El dataset completo se mantiene en RAM simultáneamente. |

### Fragmento afectado

```go
var filas []Reclamo
```

### Impacto

Con más de 1.2 millones de registros:

- alto consumo de heap
- presión de GC
- pausas de garbage collector

### Recomendación

Implementar streaming y procesamiento incremental.

---

## 4.2 Uso intensivo de strings y allocations

| Aspecto   | Detalle                                             |
| --------- | --------------------------------------------------- |
| Severidad | Media                                               |
| Problema  | Existen múltiples operaciones repetidas de strings. |

### Fragmento afectado

```go
strings.TrimSpace()
strings.ToUpper()
strings.Fields()
strings.Join()
```

### Impacto

- alto número de allocations
- presión sobre GC
- menor throughput

### Recomendación

Optimizar:

- buffers reutilizables
- sync.Pool
- evitar conversiones repetidas

---

## 4.3 Regex sobre datasets masivos

| Aspecto   | Detalle                                               |
| --------- | ----------------------------------------------------- |
| Severidad | Media                                                 |
| Problema  | El uso intensivo de regex puede degradar rendimiento. |

### Fragmento afectado

```go
reRef.FindStringSubmatch(texto)
```

### Recomendación

Reemplazar regex simples por:

```go
strings.Index()
strings.Contains()
```

cuando sea viable.

---

## 4.4 Posible desbalance de carga entre workers

| Aspecto   | Detalle                                               |
| --------- | ----------------------------------------------------- |
| Severidad | Media                                                 |
| Problema  | El tamaño fijo de lote puede generar workers ociosos. |

### Fragmento afectado

```go
const BATCH_SIZE = 1000
```

### Riesgo

- baja utilización CPU
- imbalance de procesamiento
- throughput inconsistente

### Recomendación

Implementar:

- batch adaptativo
- work stealing
- chunking dinámico

---

## 4.5 Uso subóptimo de CPU cores

| Aspecto   | Detalle                                          |
| --------- | ------------------------------------------------ |
| Severidad | Baja                                             |
| Problema  | No existe configuración explícita de GOMAXPROCS. |

### Recomendación

Evaluar:

```go
runtime.GOMAXPROCS(runtime.NumCPU())
```

Y medir benchmarking real.

---

## 4.6 Repetición innecesaria de parsing temporal

| Aspecto   | Detalle                                                                |
| --------- | ---------------------------------------------------------------------- |
| Severidad | Media                                                                  |
| Problema  | Algunas fechas son parseadas múltiples veces en distintos componentes. |

### Recomendación

Normalizar timestamps una sola vez durante ingestión.
