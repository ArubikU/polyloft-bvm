import tkinter as tk
from tkinter import ttk

BG = "#0f172a"
PANEL = "#1e293b"
CARD = "#273449"
BORDER = "#475569"
TEXT = "#e2e8f0"
MUTED = "#94a3b8"
ACCENT = "#22c55e"


class DemoApp:
    def __init__(self, root: tk.Tk) -> None:
        self.root = root
        root.title("UI Demo Tkinter")
        root.geometry("1120x760")
        root.configure(bg=BG)

        style = ttk.Style(root)
        style.theme_use("clam")

        frame = tk.Frame(root, bg=BG)
        frame.pack(fill="both", expand=True, padx=18, pady=18)

        app = tk.Frame(frame, bg=BG, highlightbackground="#1f2937", highlightthickness=1)
        app.pack(fill="both", expand=True)

        header = tk.Frame(app, bg="#111827", highlightbackground="#374151", highlightthickness=1)
        header.pack(fill="x", padx=14, pady=(14, 10))

        tk.Label(header, text="Cross Platform UI Demo", bg="#111827", fg=TEXT, font=("Segoe UI", 20, "bold")).pack(anchor="w", padx=12, pady=(10, 0))
        tk.Label(header, text="Referencia visual: HTML/CSS, PF y Tkinter con mismo contenido", bg="#111827", fg=MUTED, font=("Segoe UI", 10)).pack(anchor="w", padx=12, pady=(2, 10))

        layout = tk.Frame(app, bg=BG)
        layout.pack(fill="both", expand=True, padx=14, pady=(0, 14))
        layout.grid_columnconfigure(0, weight=2)
        layout.grid_columnconfigure(1, weight=1)
        layout.grid_rowconfigure(0, weight=1)

        left = tk.Frame(layout, bg=PANEL, highlightbackground=BORDER, highlightthickness=1)
        left.grid(row=0, column=0, sticky="nsew", padx=(0, 7))
        right = tk.Frame(layout, bg=PANEL, highlightbackground=BORDER, highlightthickness=1)
        right.grid(row=0, column=1, sticky="nsew", padx=(7, 0))

        tk.Label(left, text="OVERVIEW", bg=PANEL, fg="#cbd5e1", font=("Segoe UI", 10, "bold")).pack(anchor="w", padx=12, pady=(12, 8))

        kpi_wrap = tk.Frame(left, bg=PANEL)
        kpi_wrap.pack(fill="x", padx=10)
        for i in range(3):
            kpi_wrap.grid_columnconfigure(i, weight=1)

        self._card(kpi_wrap, 0, "Platform", "Native")
        self._card(kpi_wrap, 1, "Theme", "Dark")
        self._card(kpi_wrap, 2, "Sync CSS", "Yes")

        toolbar = tk.Frame(left, bg=PANEL)
        toolbar.pack(fill="x", padx=10, pady=10)
        tk.Button(toolbar, text="Primary", bg=ACCENT, fg="#052e16", relief="flat", padx=14, pady=8).pack(side="left", padx=(0, 8))
        tk.Button(toolbar, text="Secondary", bg="#334155", fg=TEXT, relief="flat", padx=14, pady=8).pack(side="left")

        inputs = tk.Frame(left, bg=PANEL)
        inputs.pack(fill="x", padx=10)

        e1 = tk.Entry(inputs, bg="#1f2937", fg=TEXT, insertbackground=TEXT, relief="flat")
        e1.insert(0, "Example input")
        e1.pack(fill="x", pady=(0, 8), ipady=6)

        e2 = tk.Entry(inputs, bg="#1f2937", fg=TEXT, insertbackground=TEXT, relief="flat", show="*")
        e2.insert(0, "123456")
        e2.pack(fill="x", pady=(0, 8), ipady=6)

        txt = tk.Text(inputs, height=3, bg="#1f2937", fg=TEXT, insertbackground=TEXT, relief="flat")
        txt.insert("1.0", "Multiline text area")
        txt.pack(fill="x")

        list_panel = tk.Frame(left, bg="#111827", highlightbackground=BORDER, highlightthickness=1)
        list_panel.pack(fill="both", expand=True, padx=10, pady=(12, 12))

        canvas = tk.Canvas(list_panel, bg="#111827", highlightthickness=0)
        scrollbar = ttk.Scrollbar(list_panel, orient="vertical", command=canvas.yview)
        items = tk.Frame(canvas, bg="#111827")

        items.bind("<Configure>", lambda e: canvas.configure(scrollregion=canvas.bbox("all")))
        canvas.create_window((0, 0), window=items, anchor="nw")
        canvas.configure(yscrollcommand=scrollbar.set)

        canvas.pack(side="left", fill="both", expand=True)
        scrollbar.pack(side="right", fill="y")

        for name, kind in [
            ("Bulbasaur", "Grass"),
            ("Charmander", "Fire"),
            ("Squirtle", "Water"),
            ("Pikachu", "Electric"),
            ("Eevee", "Normal"),
            ("Snorlax", "Normal"),
            ("Gengar", "Ghost"),
            ("Dragonite", "Dragon"),
        ]:
            self._row(items, name, kind)

        tk.Label(right, text="PREVIEW", bg=PANEL, fg="#cbd5e1", font=("Segoe UI", 10, "bold")).pack(anchor="w", padx=12, pady=(12, 8))
        preview = tk.Frame(right, bg="#16253f", highlightbackground="#64748b", highlightthickness=1)
        preview.pack(fill="both", expand=True, padx=12, pady=(0, 12))
        tk.Label(preview, text="Shared layout and content baseline", bg="#16253f", fg="#cbd5e1", font=("Segoe UI", 11)).place(relx=0.5, rely=0.5, anchor="center")

    def _card(self, parent: tk.Widget, col: int, label: str, value: str) -> None:
        c = tk.Frame(parent, bg=CARD, highlightbackground=BORDER, highlightthickness=1)
        c.grid(row=0, column=col, sticky="nsew", padx=4, pady=4)
        tk.Label(c, text=label, bg=CARD, fg=MUTED, font=("Segoe UI", 9)).pack(anchor="w", padx=8, pady=(8, 0))
        tk.Label(c, text=value, bg=CARD, fg=TEXT, font=("Segoe UI", 14, "bold")).pack(anchor="w", padx=8, pady=(2, 8))

    def _row(self, parent: tk.Widget, name: str, kind: str) -> None:
        r = tk.Frame(parent, bg="#334155", highlightbackground="#475569", highlightthickness=1)
        r.pack(fill="x", padx=8, pady=4)
        tk.Label(r, text=name, bg="#334155", fg=TEXT, font=("Segoe UI", 10)).pack(side="left", padx=8, pady=6)
        tk.Label(r, text=kind, bg="#1f2937", fg="#cbd5e1", font=("Segoe UI", 9), padx=8, pady=2).pack(side="right", padx=8)


if __name__ == "__main__":
    root = tk.Tk()
    DemoApp(root)
    root.mainloop()
