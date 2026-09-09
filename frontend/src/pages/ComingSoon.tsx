import { EmptyState } from '../components/ui';

export default function ComingSoon({ title }: { title: string }) {
  return (
    <div className="page page-wide">
      <header className="page-head">
        <div>
          <h1>{title}</h1>
          <p className="subtitle">Planned feature</p>
        </div>
      </header>
      <div className="card">
        <EmptyState
          title="Coming Soon"
          message={
            <>
              This area will be enabled together with its API endpoints.
              <br />
              <em>Everything is an API — the UI never exposes what the API does not.</em>
            </>
          }
        />
      </div>
    </div>
  );
}
