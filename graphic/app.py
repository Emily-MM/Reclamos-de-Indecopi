import json
from pathlib import Path

import pandas as pd
import plotly.graph_objects as go
from plotly.subplots import make_subplots
import streamlit as st


st.set_page_config(
    page_title="Comparativa de tiempos",
    page_icon="📊",
    layout="wide"
)


LOGS_DIR = Path("logs")
TIPOS_ORDEN = ["secuencial", "concurrente"]
TIPOS_TITULO = {
    "secuencial": "Secuencial",
    "concurrente": "Concurrente",
}

COLORES_LAPTOP = {
    "iam": "#2563eb",
    "emily": "#f97316",
    "jeffrey": "#059669",
}

COLORES_TIPO = {
    "secuencial": "#475569",
    "concurrente": "#0ea5e9",
}

COLOR_FALLBACK = "#64748b"


def nombre_laptop(nombre):
    return nombre.replace("_", " ").title()


def color_laptop(laptop):
    return COLORES_LAPTOP.get(laptop, COLOR_FALLBACK)


def datos_desde_nombre(ruta):
    partes = ruta.stem.split("_")

    if len(partes) < 3 or partes[0] != "logs":
        return None

    tipo = partes[1].lower()
    laptop = "_".join(partes[2:]).lower()

    if tipo not in TIPOS_TITULO:
        return None

    return tipo, laptop


def cargar_log(ruta, tipo, laptop):
    with open(ruta, "r", encoding="utf-8") as f:
        data = json.load(f)

    ejecuciones = data.get("ejecuciones", [])
    if not ejecuciones:
        raise ValueError(f"{ruta.name} no contiene ejecuciones.")

    df = pd.DataFrame(ejecuciones)
    for columna in ["run", "tiempo_s", "heap_mb"]:
        if columna not in df:
            df[columna] = pd.NA

    df["tipo"] = tipo
    df["tipo_titulo"] = TIPOS_TITULO[tipo]
    df["laptop"] = laptop
    df["laptop_titulo"] = nombre_laptop(laptop)
    df["archivo"] = ruta.name
    df["fecha"] = data.get("fecha")
    df["total_filas"] = data.get("total_filas")
    df["total_ejecuciones"] = data.get("total_ejecuciones")
    df["tiempo_min_s"] = data.get("tiempo_min_s")
    df["tiempo_max_s"] = data.get("tiempo_max_s")
    df["media_recortada_s"] = data.get("media_recortada_s")
    df["heap_antes_mb"] = data.get("heap_antes_mb")
    df["heap_despues_mb"] = data.get("heap_despues_mb")
    df["nucleos_cpu"] = data.get("nucleos_cpu")
    df["workers"] = data.get("workers")
    df["tamano_lote"] = data.get("tamano_lote")
    return df


def cargar_logs():
    logs = []
    errores = []

    for ruta in sorted(LOGS_DIR.glob("logs_*_*.json")):
        datos = datos_desde_nombre(ruta)

        if datos is None:
            continue

        tipo, laptop = datos
        try:
            logs.append(cargar_log(ruta, tipo, laptop))
        except (json.JSONDecodeError, OSError, ValueError) as exc:
            errores.append(f"{ruta.name}: {exc}")

    return logs, errores


def calcular_speedup(fila):
    secuencial = fila.get("secuencial")
    concurrente = fila.get("concurrente")

    if pd.isna(secuencial) or pd.isna(concurrente) or concurrente <= 0:
        return pd.NA

    return secuencial / concurrente


def construir_resumen(df):
    resumen = (
        df.groupby(["laptop", "laptop_titulo", "tipo"], as_index=False)
        .agg(
            media_recortada_s=("media_recortada_s", "first"),
            promedio_simple_s=("tiempo_s", "mean"),
            minimo_s=("tiempo_min_s", "first"),
            maximo_s=("tiempo_max_s", "first"),
            heap_promedio_mb=("heap_mb", "mean"),
            heap_antes_mb=("heap_antes_mb", "first"),
            heap_despues_mb=("heap_despues_mb", "first"),
            nucleos_cpu=("nucleos_cpu", "first"),
            workers=("workers", "first"),
            tamano_lote=("tamano_lote", "first"),
            archivo=("archivo", "first"),
            fecha=("fecha", "first"),
        )
    )

    tabla_recortada = resumen.pivot(
        index=["laptop", "laptop_titulo"],
        columns="tipo",
        values="media_recortada_s"
    ).reset_index()

    for tipo in TIPOS_ORDEN:
        if tipo not in tabla_recortada:
            tabla_recortada[tipo] = pd.NA

    tabla_recortada["speedup"] = tabla_recortada.apply(calcular_speedup, axis=1)
    return resumen, tabla_recortada


def valor_metrica(valor, formato="{:.4f}", sufijo=""):
    if pd.isna(valor):
        return "N/D"
    return f"{formato.format(valor)}{sufijo}"


def aplicar_estilo(fig, alto=760, margen_derecho=170, titulo_leyenda="Laptops"):
    fig.update_layout(
        template="none",
        height=alto,
        hovermode="x unified",
        showlegend=True,
        paper_bgcolor="#f8fafc",
        plot_bgcolor="#ffffff",
        font=dict(color="#0f172a", family="Arial, sans-serif", size=13),
        legend=dict(
            title=titulo_leyenda,
            orientation="v",
            yanchor="top",
            y=1,
            xanchor="left",
            x=1.02,
            bgcolor="#ffffff",
            bordercolor="#cbd5e1",
            borderwidth=1,
            font=dict(color="#0f172a", size=13),
            title_font=dict(color="#0f172a", size=14),
            itemclick="toggle",
            itemdoubleclick="toggleothers",
        ),
        hoverlabel=dict(
            bgcolor="#ffffff",
            bordercolor="#cbd5e1",
            font_size=13,
            font_family="Arial, sans-serif",
        ),
        margin=dict(l=54, r=margen_derecho, t=54, b=54),
    )

    fig.update_annotations(font=dict(size=15, color="#0f172a"))
    fig.update_xaxes(
        showgrid=True,
        gridcolor="#e2e8f0",
        zeroline=False,
        showline=True,
        linecolor="#94a3b8",
        ticks="outside",
        tickfont=dict(color="#0f172a", size=12),
        title_font=dict(color="#0f172a", size=13),
    )
    fig.update_yaxes(
        showgrid=True,
        gridcolor="#e2e8f0",
        zeroline=False,
        showline=True,
        linecolor="#94a3b8",
        ticks="outside",
        tickfont=dict(color="#0f172a", size=12),
        title_font=dict(color="#0f172a", size=13),
    )


def crear_figura_resumen(tabla_recortada):
    fig = make_subplots(
        rows=1,
        cols=2,
        subplot_titles=(
            "Media recortada por laptop",
            "Speedup por laptop",
        ),
        horizontal_spacing=0.16,
    )

    for tipo in TIPOS_ORDEN:
        fig.add_trace(
            go.Bar(
                x=tabla_recortada["laptop_titulo"],
                y=tabla_recortada[tipo],
                name=TIPOS_TITULO[tipo],
                marker_color=COLORES_TIPO[tipo],
                hovertemplate=(
                    f"<b>{TIPOS_TITULO[tipo]}</b><br>"
                    "Laptop: %{x}<br>"
                    "Media recortada: %{y:.4f} s"
                    "<extra></extra>"
                ),
            ),
            row=1,
            col=1,
        )

    fig.add_trace(
        go.Bar(
            x=tabla_recortada["laptop_titulo"],
            y=tabla_recortada["speedup"],
            name="Speedup",
            marker_color="#7c3aed",
            showlegend=False,
            hovertemplate=(
                "<b>Speedup</b><br>"
                "Laptop: %{x}<br>"
                "Speedup: %{y:.2f}x"
                "<extra></extra>"
            ),
        ),
        row=1,
        col=2,
    )

    aplicar_estilo(fig, alto=430, margen_derecho=130, titulo_leyenda="Versión")
    fig.update_xaxes(title_text="Laptop", row=1, col=1)
    fig.update_xaxes(title_text="Laptop", row=1, col=2)
    fig.update_yaxes(title_text="Segundos", row=1, col=1)
    fig.update_yaxes(title_text="Veces más rápido", row=1, col=2)
    return fig


def crear_figura_heap_resumen(resumen):
    fig = go.Figure()
    resumen_ordenado = resumen.sort_values(["tipo", "laptop_titulo"])

    for tipo in TIPOS_ORDEN:
        df_tipo = resumen_ordenado[resumen_ordenado["tipo"] == tipo]

        fig.add_trace(
            go.Bar(
                x=df_tipo["laptop_titulo"],
                y=df_tipo["heap_promedio_mb"],
                name=TIPOS_TITULO[tipo],
                marker_color=COLORES_TIPO[tipo],
                hovertemplate=(
                    f"<b>{TIPOS_TITULO[tipo]}</b><br>"
                    "Laptop: %{x}<br>"
                    "Heap promedio: %{y:.2f} MB"
                    "<extra></extra>"
                ),
            )
        )

    aplicar_estilo(fig, alto=420, margen_derecho=130, titulo_leyenda="Versión")
    fig.update_layout(barmode="group")
    fig.update_xaxes(title_text="Laptop")
    fig.update_yaxes(title_text="Heap promedio (MB)")
    return fig


def crear_figura_tiempos(df, laptops):
    fig = make_subplots(
        rows=2,
        cols=1,
        shared_xaxes=True,
        subplot_titles=(
            "Tiempo de ejecución secuencial por laptop",
            "Tiempo de ejecución concurrente por laptop",
        ),
        vertical_spacing=0.13,
    )

    for fila, tipo in enumerate(TIPOS_ORDEN, start=1):
        for laptop in laptops:
            df_linea = df[(df["tipo"] == tipo) & (df["laptop"] == laptop)]

            if df_linea.empty:
                continue

            laptop_titulo = df_linea["laptop_titulo"].iloc[0]
            media_recortada = df_linea["media_recortada_s"].iloc[0]
            color = color_laptop(laptop)

            fig.add_trace(
                go.Scatter(
                    x=df_linea["run"],
                    y=df_linea["tiempo_s"],
                    mode="lines+markers",
                    name=laptop_titulo,
                    legendgroup=laptop,
                    line=dict(width=2.7, color=color),
                    marker=dict(
                        size=5,
                        color=color,
                        line=dict(color="#ffffff", width=0.8),
                    ),
                    showlegend=tipo == "secuencial",
                    hovertemplate=(
                        f"<b>{TIPOS_TITULO[tipo]} - {laptop_titulo}</b><br>"
                        "Ejecución: %{x}<br>"
                        "Tiempo: %{y:.6f} s<br>"
                        f"Media recortada: {media_recortada:.6f} s"
                        "<extra></extra>"
                    ),
                ),
                row=fila,
                col=1,
            )

            fig.add_trace(
                go.Scatter(
                    x=[df_linea["run"].min(), df_linea["run"].max()],
                    y=[media_recortada, media_recortada],
                    mode="lines",
                    name=f"Media recortada - {laptop_titulo}",
                    legendgroup=laptop,
                    line=dict(width=1.6, color=color, dash="dash"),
                    opacity=0.75,
                    showlegend=False,
                    hovertemplate=(
                        f"<b>Media recortada - {TIPOS_TITULO[tipo]} - "
                        f"{laptop_titulo}</b><br>"
                        f"{media_recortada:.6f} s"
                        "<extra></extra>"
                    ),
                ),
                row=fila,
                col=1,
            )

    aplicar_estilo(fig)
    fig.update_xaxes(title_text="Número de ejecución", row=2, col=1)
    fig.update_yaxes(title_text="Tiempo en segundos", row=1, col=1)
    fig.update_yaxes(title_text="Tiempo en segundos", row=2, col=1)
    return fig


def crear_figura_heap(df, laptops):
    fig = make_subplots(
        rows=2,
        cols=1,
        shared_xaxes=True,
        subplot_titles=(
            "Heap secuencial por laptop",
            "Heap concurrente por laptop",
        ),
        vertical_spacing=0.13,
    )

    for fila, tipo in enumerate(TIPOS_ORDEN, start=1):
        for laptop in laptops:
            df_linea = df[(df["tipo"] == tipo) & (df["laptop"] == laptop)]

            if df_linea.empty or df_linea["heap_mb"].isna().all():
                continue

            laptop_titulo = df_linea["laptop_titulo"].iloc[0]
            heap_promedio = df_linea["heap_mb"].mean()
            color = color_laptop(laptop)

            fig.add_trace(
                go.Scatter(
                    x=df_linea["run"],
                    y=df_linea["heap_mb"],
                    mode="lines+markers",
                    name=laptop_titulo,
                    legendgroup=laptop,
                    line=dict(width=2.5, color=color),
                    marker=dict(
                        size=5,
                        color=color,
                        line=dict(color="#ffffff", width=0.8),
                    ),
                    showlegend=tipo == "secuencial",
                    hovertemplate=(
                        f"<b>{TIPOS_TITULO[tipo]} - {laptop_titulo}</b><br>"
                        "Ejecución: %{x}<br>"
                        "Heap: %{y:.2f} MB<br>"
                        f"Heap promedio: {heap_promedio:.2f} MB"
                        "<extra></extra>"
                    ),
                ),
                row=fila,
                col=1,
            )

    aplicar_estilo(fig)
    fig.update_xaxes(title_text="Número de ejecución", row=2, col=1)
    fig.update_yaxes(title_text="Heap en MB", row=1, col=1)
    fig.update_yaxes(title_text="Heap en MB", row=2, col=1)
    return fig


st.title("Comparativa de rendimiento")
st.caption("Resultados por laptop para las versiones secuencial y concurrente.")

if not LOGS_DIR.exists():
    st.error("No existe la carpeta logs con los archivos JSON.")
    st.stop()

logs, errores = cargar_logs()

if not logs:
    st.error(
        "No se encontraron archivos en logs/ con el formato "
        "logs_secuencial_nombre.json o logs_concurrente_nombre.json."
    )
    st.stop()

if errores:
    st.warning("Algunos logs no se pudieron cargar:\n\n" + "\n".join(errores))

df_completo = pd.concat(logs, ignore_index=True).sort_values(["tipo", "laptop", "run"])
laptops_disponibles = sorted(df_completo["laptop"].unique())
laptops_por_titulo = {nombre_laptop(laptop): laptop for laptop in laptops_disponibles}

st.sidebar.header("Filtros")
st.sidebar.caption(f"Carpeta de datos: `{LOGS_DIR}`")
seleccion_titulos = st.sidebar.multiselect(
    "Laptops a comparar",
    options=list(laptops_por_titulo.keys()),
    default=list(laptops_por_titulo.keys()),
)
st.sidebar.caption(
    "Selecciona una o más laptops para actualizar las tablas y gráficas."
)

laptops = [laptops_por_titulo[titulo] for titulo in seleccion_titulos]

if not laptops:
    st.warning("Selecciona al menos una laptop en la barra lateral.")
    st.stop()

df = df_completo[df_completo["laptop"].isin(laptops)]
resumen, tabla_recortada = construir_resumen(df)

media_sec = resumen[resumen["tipo"] == "secuencial"]["media_recortada_s"].mean()
media_con = resumen[resumen["tipo"] == "concurrente"]["media_recortada_s"].mean()
speedup = media_sec / media_con if pd.notna(media_con) and media_con > 0 else pd.NA
total_runs = int(df["run"].count())
total_archivos = df["archivo"].nunique()

col1, col2, col3, col4 = st.columns(4)

col1.metric("Laptops visibles", len(laptops))
col2.metric("Media secuencial", valor_metrica(media_sec, sufijo=" s"))
col3.metric("Media concurrente", valor_metrica(media_con, sufijo=" s"))
col4.metric("Speedup", valor_metrica(speedup, formato="{:.2f}", sufijo="x"))

st.caption(
    f"Fuente: {total_archivos} archivos JSON, {total_runs} ejecuciones. "
    "Métrica base: `media_recortada_s`."
)

tab_resumen, tab_tiempos, tab_heap = st.tabs(["Resumen", "Tiempos", "Heap"])

with tab_resumen:
    st.subheader("Resumen de tiempo")
    st.caption(
        "Comparación consolidada de tiempos y aceleración por laptop."
    )

    tabla_mostrar = tabla_recortada[
        ["laptop_titulo", "secuencial", "concurrente", "speedup"]
    ].rename(
        columns={
            "laptop_titulo": "Laptop",
            "secuencial": "Media recortada secuencial (s)",
            "concurrente": "Media recortada concurrente (s)",
            "speedup": "Speedup",
        }
    )

    st.dataframe(
        tabla_mostrar,
        width="stretch",
        hide_index=True,
        column_config={
            "Media recortada secuencial (s)": st.column_config.NumberColumn(
                format="%.4f"
            ),
            "Media recortada concurrente (s)": st.column_config.NumberColumn(
                format="%.4f"
            ),
            "Speedup": st.column_config.NumberColumn(format="%.2fx"),
        },
    )

    st.plotly_chart(crear_figura_resumen(tabla_recortada), width="stretch")

    st.subheader("Resumen de heap")
    st.caption("Memoria promedio registrada por versión y laptop.")

    tabla_heap_resumen = resumen[
        [
            "laptop_titulo",
            "tipo",
            "heap_promedio_mb",
            "heap_antes_mb",
            "heap_despues_mb",
            "nucleos_cpu",
            "workers",
            "tamano_lote",
        ]
    ].copy()
    tabla_heap_resumen["tipo"] = tabla_heap_resumen["tipo"].map(TIPOS_TITULO)
    tabla_heap_resumen = tabla_heap_resumen.rename(
        columns={
            "laptop_titulo": "Laptop",
            "tipo": "Tipo",
            "heap_promedio_mb": "Heap promedio (MB)",
            "heap_antes_mb": "Heap inicial (MB)",
            "heap_despues_mb": "Heap final (MB)",
            "nucleos_cpu": "CPU",
            "workers": "Workers",
            "tamano_lote": "Tamaño de lote",
        }
    )
    for columna in ["Workers", "Tamaño de lote"]:
        tabla_heap_resumen[columna] = tabla_heap_resumen[columna].apply(
            lambda valor: "-" if pd.isna(valor) else str(int(valor))
        )

    st.dataframe(
        tabla_heap_resumen,
        width="stretch",
        hide_index=True,
        column_config={
            "Heap promedio (MB)": st.column_config.NumberColumn(format="%.2f"),
            "Heap inicial (MB)": st.column_config.NumberColumn(format="%.2f"),
            "Heap final (MB)": st.column_config.NumberColumn(format="%.2f"),
        },
    )

    st.plotly_chart(crear_figura_heap_resumen(resumen), width="stretch")

with tab_tiempos:
    st.subheader("Histórico de tiempos")
    st.caption(
        "Las líneas sólidas representan ejecuciones individuales; las punteadas, "
        "la media recortada."
    )
    st.plotly_chart(crear_figura_tiempos(df, laptops), width="stretch")

with tab_heap:
    st.subheader("Histórico de heap")
    st.caption(
        "Evolución de memoria registrada durante las ejecuciones."
    )
    st.plotly_chart(crear_figura_heap(df, laptops), width="stretch")
