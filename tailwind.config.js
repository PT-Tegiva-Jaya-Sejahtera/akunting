/** Konfigurasi Tailwind: token desain "Finansial Anggun".
 *  Tipografi: bobot teks normal 400 (ringan hanya untuk display besar),
 *  angka monospace tabular, aksen ungu ala Stripe.
 *  Skala radius konsentris: input 6px -> kartu 10px -> wadah besar 16px. */
module.exports = {
  // Kelas juga muncul di berkas pendamping Go (helper kelas) dan di skrip
  // klien (tooltip grafik, kalender), jadi keduanya ikut dipindai. Beberapa
  // kelas khusus daftar safelist di bawah supaya selalu ada di app.css.
  content: ["./templates/**/*.templ", "./templates/**/*.go", "./static/js/*.js"],
  safelist: [
    "rounded-[6px]",
    "max-w-[240px]",
    "w-max",
    "pointer-events-none",
    "grid-cols-7",
    "text-[10px]",
    "bottom-full",
    "absolute",
    "right-0",
    "left-0",
    "w-64",
  ],
  theme: {
    extend: {
      colors: {
        ink: "#061b31",
        label: "#273951",
        muted: "#5b6b83",
        line: "#e3ebf3",
        panel: "#f6f9fc",
        accent: {
          DEFAULT: "#533afd",
          hover: "#4434d4",
          soft: "#b9b9f9",
          wash: "rgba(83,58,253,0.06)",
        },
        ok: "#0f7a35",
        danger: "#b01e48",
      },
      fontFamily: {
        sans: ["'Inter'", "system-ui", "-apple-system", "'Segoe UI'", "sans-serif"],
        display: ["'Plus Jakarta Sans'", "'Inter'", "system-ui", "sans-serif"],
        mono: ["'Source Code Pro'", "ui-monospace", "SFMono-Regular", "monospace"],
      },
      fontWeight: {
        light: "300",
        normal: "400",
        medium: "500",
        semibold: "600",
      },
      borderRadius: {
        DEFAULT: "6px",
        input: "6px",
        card: "10px",
        nav: "7px",
        lg: "12px",
        xl: "16px",
      },
      boxShadow: {
        input: "0 1px 2px 0 rgba(6,27,49,0.04)",
        card: "0 2px 4px rgba(6,27,49,0.04), 0 8px 24px -12px rgba(6,27,49,0.12)",
        dropdown:
          "0 1px 1px rgba(6,27,49,0.05), 0 8px 16px -6px rgba(6,27,49,0.14), 0 16px 48px -16px rgba(6,27,49,0.18)",
        raised:
          "0 2px 6px rgba(6,27,49,0.06), 0 18px 48px -12px rgba(6,27,49,0.28)",
        btn: "0 1px 1px rgba(38,34,98,0.18), 0 4px 12px -4px rgba(83,58,253,0.4)",
        btnHover:
          "0 2px 2px rgba(38,34,98,0.2), 0 8px 20px -6px rgba(83,58,253,0.45)",
      },
      typography: {},
    },
  },
  plugins: [],
};
