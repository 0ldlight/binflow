// Home is the console's landing screen (T-89 scaffold placeholder). The
// information architecture and real screens land with the console UI tickets
// (ux-designer spec, M4); this page only proves the mount, router and lazy
// chunk chain end to end.
export default function Home() {
  return (
    <main className="home">
      <h1>BinFlow Console</h1>
      <p data-testid="console-scaffold">scaffold ready</p>
    </main>
  )
}
