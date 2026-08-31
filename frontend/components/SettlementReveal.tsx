import type { JSX } from "react";
import type { ResultRow } from "../lib/protocol";

export type SettlementRevealProps = {
  results: ResultRow[] | null;
  outcomes: string[];
  winningOutcome: number | null;
  dust: number;
  refunded: boolean;
  refundTotal: number | null;
  selfId: string | null;
};

function formatNet(net: number): string {
  return net > 0 ? `+${net}` : `${net}`;
}

export function SettlementReveal({
  results,
  outcomes,
  winningOutcome,
  dust,
  refunded,
  refundTotal,
  selfId,
}: SettlementRevealProps): JSX.Element | null {
  if (results === null && !refunded) {
    return null;
  }

  const winningLabel = winningOutcome !== null ? outcomes[winningOutcome] : null;

  return (
    <div>
      {winningLabel !== null && <h2>{winningLabel}</h2>}
      {results !== null && refunded && (
        <p>Nobody backed the winning outcome — every stake was returned</p>
      )}
      {results === null && refundTotal !== null && (
        <p>The round went unresolved — all {refundTotal} tokens were refunded</p>
      )}
      {results !== null && (
        <table>
          <tbody>
            {results.map((row) => (
              <tr key={row.user_id}>
                <td>
                  {row.display_name}
                  {row.user_id === selfId ? " (you)" : ""}
                </td>
                <td>{row.staked}</td>
                <td>{row.returned}</td>
                <td>{formatNet(row.net)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {results !== null && <p>Dust: {dust}</p>}
    </div>
  );
}
