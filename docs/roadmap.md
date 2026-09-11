# Roadmap & Arquitectura: Sucesor Espiritual de FotoDif en Go

Este documento sirve como guía de diseño técnico, hoja de ruta y especificación para el desarrollo de una herramienta moderna de fotometría diferencial de apertura escrita en **Go (Golang)** bajo licencia **Apache 2.0**.

---

## 1. Objetivos del Proyecto
*   **Velocidad y Concurrencia:** Aprovechar el modelo de *goroutines* de Go para procesar cientos de imágenes FITS en paralelo.
*   **Portabilidad:** Compilar en un único binario nativo sin dependencias externas pesadas (fácil distribución).
*   **Código Abierto:** Publicar en `fikua.org` con auditoría completa de la comunidad científica y astronómica.

---

## 2. Dependencias Clave en el Ecosistema Go

Para no reinventar la rueda en el manejo de archivos y cálculos astronómicos básicos, utilizaremos los siguientes paquetes del ecosistema de Go:

*   **Lectura/Escritura FITS:** `codeberg.org/astrogo/fitsio` (Librería nativa para interactuar con cabeceras y arrays de datos FITS).
*   **Cálculos Efemérides/Tiempo:** `github.com/cosinekitty/astronomy` (Motor de cálculo para tiempo sidéreo, fechas julianas y coordenadas astrométricas).
*   **Interfaz Gráfica:** `fyne.io/fyne/v2` (GUI nativa multiplataforma — decisión de arquitectura, ver sección 3).
*   **Gráficos (curvas de luz, airmass/transparencia/FWHM):** `gonum/plot`, renderizado a imagen y mostrado en un `canvas.Image` de Fyne.

---

## 3. Decisiones de Arquitectura y UI

### 3.1 Aplicación de escritorio, no SaaS web
El producto es una **aplicación instalable** (Windows/Linux/macOS), no un servicio web con cuentas de usuario:
*   Las sesiones de astrofotografía generan volúmenes grandes (FITS de 4-50 MB/imagen, sesiones de 5-20 GB). Aceptar subidas de este volumen desde muchos usuarios implica storage, egress y cómputo escalando linealmente con el uso, sin modelo de ingresos que lo sostenga.
*   Procesar y almacenar en el PC del propio usuario evita ese coste y esa complejidad de infraestructura (colas de trabajo, storage tipo S3, timeouts de subida).
*   Un posible "plus" futuro en la nube se limitaría a compartir/publicar resultados ya procesados (curva de luz, informe AAVSO/ALCDEF), nunca los FITS originales.

### 3.2 GUI nativa con Fyne (no navegador local)
Se descarta servir una UI HTML vía servidor local embebido. Motivos:
*   Autocontenido de verdad: sin depender del navegador por defecto del usuario, sin gestión de puertos.
*   Diálogos de fichero nativos del SO (`dialog.ShowFileOpen`) para seleccionar carpetas de imágenes FITS.
*   Multi-ventana nativo (imagen + gráficos abiertos a la vez), igual que el flujo de FotoDif.
*   Empaquetado (`fyne package`) genera `.exe`/`.app`/`.AppImage` con icono propio — se percibe como aplicación instalada, no un script que abre una pestaña de navegador.

### 3.3 Estética: moderna por defecto, con modo "Observador" (luz roja)
*   Fyne aporta una estética moderna (Material Design-like) por defecto — no replicamos el aspecto Windows Forms/Delphi de FotoDif.
*   Temas claro/oscuro nativos de Fyne.
*   **Modo Observador:** tema personalizado (`fyne.Theme` propio) con paleta en rojos/naranjas sobre fondo negro, pensado para preservar la visión nocturna del astrónomo en el telescopio. Conmutable en caliente desde la UI (`app.Settings().SetTheme(...)`), sin reiniciar la aplicación.

### 3.4 Gráficos: estáticos, no interactivos (igual que FotoDif)
Los gráficos de FotoDif (curva de luz, airmass/transparencia/FWHM) son estáticos, no interactivos. Replicamos ese comportamiento con `gonum/plot`, renderizando el gráfico como imagen y mostrándolo en un `canvas.Image` de Fyne — sin necesidad de una librería de charting interactiva, que en el ecosistema Fyne está poco madura.

### 3.5 Distribución y firma de código
*   **Windows:** sin firmar, SmartScreen avisa ("Run anyway") pero no bloquea — válido para el lanzamiento inicial. Firmar más adelante requiere certificado de firma de código (OV/EV), ~70-250 €/año.
*   **macOS:** Gatekeeper bloquea de forma más estricta sin firma/notarización. Requiere Apple Developer Program (99 $/año) + notarización (`codesign` + subida a Apple + stapling). Sin esto, el usuario debe hacer clic derecho → Abrir manualmente.
*   **Linux:** no hay firma de código obligatoria a nivel de SO; sin coste.
*   Se pospone la firma de Windows/macOS hasta validar tracción real del proyecto.

---

## 4. Estructura de Archivos Recomendada (Layout Estándar en Go)

```text
starflux/
├── .github/workflows/      # CI/CD para compilar binarios automáticamente
├── cmd/
│   └── starflux/           # Punto de entrada principal (main.go)
├── internal/
│   ├── fits/               # Parseo y extracción de metadatos/píxeles
│   ├── photometry/         # Algoritmos de centroide y fotometría de apertura
│   ├── alignment/          # Alineación de imágenes (WCS o patrones de estrellas)
│   └── timeseries/         # Cálculo de magnitudes relativas y curvas de luz
├── LICENSE                 # Texto de la Licencia Apache 2.0
├── README.md
├── go.mod
└── go.sum
```

---

## 5. Algoritmo Core: Fotometría de Apertura Manual

El núcleo matemático que debes programar en `internal/photometry/` consta de tres pasos principales por cada estrella seleccionada (Target, Comparación, Chequeo):

### Paso 1: Centrado por Centroide (Centro de Masas)
Dado un píxel aproximado $(x_0, y_0)$ introducido por el usuario o detectado por un buscador de fuentes, se extrae una submatriz (caja de centrado) y se refina la posición exacta:

$$X_{centro} = \frac{\sum (I_{i,j} \cdot x_{i,j})}{\sum I_{i,j}}$$
$$Y_{centro} = \frac{\sum (I_{i,j} \cdot y_{i,j})}{\sum I_{i,j}}$$

*Donde $I_{i,j}$ es la intensidad del píxel (ADUs) tras restar un nivel base de ruido.*

### Paso 2: Suma de la Apertura
Se genera un círculo virtual de radio $r_{ap}$ centrado en $(X_{centro}, Y_{centro})$.
*   Se suman todos los ADUs de los píxeles que caen completamente dentro del radio.
*   *Nota de optimización:* Para los píxeles del borde, calcula la fracción del píxel que queda dentro del círculo para una precisión de milimagnitudes.

### Paso 3: Sustracción del Fondo del Cielo (Anillo)
Se define un anillo exterior concéntrico entre $r_{in}$ y $r_{out}$ (donde ya no hay luz de la estrella).
1.  Se extraen los valores de todos los píxeles dentro del anillo.
2.  Se calcula la **mediana** (o media con *sigma-clipping* para descartar estrellas de fondo o rayos cósmicos). Este valor representará el *Fondo del Cielo por píxel* ($B_{sky}$).
3.  El flujo neto de la estrella se calcula como:

$$\text{Flujo Neto} = \text{Suma Apertura} - (N_{p\text{í}xeles\_apertura} \cdot B_{sky})$$

---

## 6. Código Base Inicial (Template en Go)

Crea este archivo en `cmd/starflux/main.go` para validar que puedes leer un archivo FITS y acceder a su matriz de píxeles:

```go
package main

import (
	"fmt"
	"log"
	"os"

	"codeberg.org/astrogo/fitsio"
)

func main() {
	// 1. Abrir el archivo FITS de prueba
	file, err := os.Open("test_image.fits")
	if err != nil {
		log.Fatalf("Error al abrir el archivo FITS: %v", err)
	}
	defer file.Close()

	r, err := fitsio.Open(file)
	if err != nil {
		log.Fatalf("Error al parsear la estructura FITS: %v", err)
	}
	defer r.Close()

	// 2. Leer la Unidad de Datos de Cabecera Primaria (HDU)
	hdu := r.HDU(0)
	header := hdu.Header()

	fmt.Println("--- Información del FITS ---")
	fmt.Printf("Instrumento/Telescopio: %v\n", header.Get("INSTRUME"))
	fmt.Printf("Tiempo de Exposición: %v segundos\n", header.Get("EXPTIME"))
	fmt.Printf("Fecha/Hora Observación: %v\n", header.Get("DATE-OBS"))

	// 3. Obtener la matriz de datos de la imagen
	imageHDU, ok := hdu.(fitsio.Image)
	if !ok {
		log.Fatalf("El HDU primario no contiene una imagen válida")
	}

	// Mostrar dimensiones de la imagen (ej: [2048, 2048])
	axes := imageHDU.Axes()
	fmt.Printf("Dimensiones de la imagen: %d x %d\n", axes[0], axes[1])

	// Aquí procesarías los píxeles usando imageHDU.Read()
}
```

---

## 7. Estrategia de Validación Cruzada (Vs FotoDif)

Para asegurar que tu software bajo Apache 2.0 es preciso, debes programar un flujo de test automatizado:
1.  **Fijar Variables:** Usa el mismo radio de apertura (ej: 6 px), anillo interno (ej: 10 px) y anillo externo (ej: 15 px) en ambos programas.
2.  **Generar Output de FotoDif:** Procesa una serie temporal de muestra en FotoDif y exporta el archivo de texto de resultados conteniendo la columna de Magnitud Relativa ($\Delta m = m_{target} - m_{comp}$).
3.  **Generar Output en Go:** Ejecuta tu código Go sobre la misma serie de FITS e imprime un archivo estructurado idéntico.
4.  **Test Estadístico (Diferencia de Residuos):** Compara ambas columnas mediante un script externo o test unitario en Go. La diferencia absoluta media debe ser inferior a $0.002$ magnitudes (tolerancia por diferencias sutiles de redondeo o cálculo de la mediana).
