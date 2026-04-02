# Falencies

Estado: en progreso
Objetivo: acercar Polyloft/Gio al comportamiento de HTML/CSS real, corrigiendo primero fallas estructurales del motor.

## Lista priorizada

1. `:root` se aplica como regla completa a todos los nodos en vez de actuar principalmente como contenedor de variables globales.
Estado: resuelto
Impacto: fuga de estilos no heredables y resultados distintos a CSS real.

2. Los `input` de texto no son realmente controlados.
Estado: resuelto
Impacto: cambios externos a `value`/`text` no re-sincronizan el `widget.Editor`.

3. Los gradientes siguen siendo una aproximacion parcial.
Estado: resuelto
Impacto: se elimino el render por steps fijos; ahora hay interpolacion matematica por pixel/radio para linear y radial con todos los color-stops.

4. El parser auxiliar de colores/argumentos funcionales sigue siendo fragil con comas y funciones anidadas.
Estado: resuelto
Impacto: degradados y colores compuestos pueden parsearse mal.

5. Los botones no respetan bien padding interno al dibujar texto.
Estado: resuelto
Impacto: labels apretados, cortados o visualmente descentrados.

6. El overflow/scroll calcula limites usando solo cajas de layout de hijos.
Estado: en progreso
Impacto: scrollbars y limites incorrectos para contenido real.

Nota: se corrigio acumulacion de scroll fraccional en Gio para touchpad/rueda de alta resolucion.
Nota: se agrego enrutamiento de wheel/touchpad al contenedor scrollable bajo el cursor (hit-test por rect), y captura de eventos para todos los nodos visibles.

7. `range`/`slider` aun no replica semantica completa de HTML.
Estado: pendiente
Impacto: falta `step`, teclado, foco y mejor hit-area.

11. Scroll por teclado en contenedores
Estado: resuelto
Impacto: ahora hay soporte para Up/Down/Left/Right, PageUp/PageDown y Home/End en contenedor scrollable enfocado.

8. Falta soporte real de backgrounds compuestos.
Estado: pendiente
Impacto: no hay paridad con `background-size`, `position`, multiples capas, etc.

9. Transitions y animaciones siguen siendo experimentales y limitados.
Estado: pendiente
Impacto: no hay paridad con animaciones CSS completas.
Detalle: solo se animan cambios de estilo directos, sin keyframes ni animaciones independientes. faltaria tambien cosas como active y hover.

10. Diseñar sistema de hover y lector de pantalla
Estado: pendiente
Impacto: sin hover ni accesibilidad, la UI es limitada y no apta para producción.