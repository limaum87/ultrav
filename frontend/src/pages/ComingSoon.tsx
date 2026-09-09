export default function ComingSoon({ title }: { title: string }) {
  return (
    <div className="page">
      <header className="page-head">
        <h1>{title}</h1>
      </header>
      <div className="card coming-soon">
        <div className="coming-icon">◍</div>
        <h2>Coming Soon</h2>
        <p>
          This area will be enabled together with its API endpoints.
          <br />
          <em>Everything is an API — the UI never exposes what the API does not.</em>
        </p>
      </div>
    </div>
  );
}
