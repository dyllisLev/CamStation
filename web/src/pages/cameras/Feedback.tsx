export function EmptyState() {
  return (
    <div className="new-empty">
      호스트와 계정을 입력한 뒤 프로파일 스캔을 실행하세요.
    </div>
  );
}

export function MutationError({ message }: { message?: string }) {
  if (!message) return null;
  return <div className="new-form-error">{message}</div>;
}
