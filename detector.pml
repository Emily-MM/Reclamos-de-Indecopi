#define NUM_WORKERS  2
#define NUM_LOTES    3
#define UMBRAL_SPAM  3    
#define UMBRAL_PICO  5   

int  alertas_global = 0;
chan lotes_canal = [NUM_LOTES] of { int };

active proctype dispatcher() {
    int i = 0;

    do
    :: i < NUM_LOTES ->
        lotes_canal ! i;
        i++
    :: i >= NUM_LOTES ->
        break
    od;

    int w = 0;
    do
    :: w < NUM_WORKERS ->
        lotes_canal ! -1;
        w++
    :: w >= NUM_WORKERS ->
        break
    od
}

active [NUM_WORKERS] proctype worker() {
    int lote_id;
    int conteo_texto;  
    int conteo_pico;    
    int alertas_spam;
    int alertas_pico;
    int alertas_locales;

    do
    :: lotes_canal ? lote_id ->

        if
        :: lote_id == -1 -> break
        :: else ->

            if
            :: conteo_texto = 2    
            :: conteo_texto = 4    
            fi;

            if
            :: conteo_texto >= UMBRAL_SPAM -> alertas_spam = 1
            :: else                        -> alertas_spam = 0
            fi;

            if
            :: conteo_pico = 3    
            :: conteo_pico = 6    
            fi;

            if
            :: conteo_pico >= UMBRAL_PICO -> alertas_pico = 1
            :: else                       -> alertas_pico = 0
            fi;

            alertas_locales = alertas_spam + alertas_pico;

            if
            :: alertas_locales > 0 ->
                atomic {
                    alertas_global = alertas_global + alertas_locales
                }
            :: else -> skip
            fi

        fi
    od
}

active proctype monitor() {
    do
    :: true ->
        assert(alertas_global >= 0);
        assert(alertas_global <= NUM_LOTES * NUM_WORKERS * 2)
    od
}
