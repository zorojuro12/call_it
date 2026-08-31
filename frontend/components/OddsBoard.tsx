export type OddsBoardProps = {
  outcomes: string[];
  pools: number[];
  total: number;
  multipliers: number[];
  bettors: number;
  players: number;
  winningOutcome?: number | null;
};

export function OddsBoard({
  outcomes,
  pools,
  total,
  multipliers,
}: OddsBoardProps) {
  return (
    <div>
      <table>
        <thead>
          <tr>
            <th scope="col">Outcome</th>
            <th scope="col">Pool</th>
            <th scope="col">Multiplier</th>
          </tr>
        </thead>
        <tbody>
          {outcomes.map((outcome, i) => (
            <tr key={outcome}>
              <td>{outcome}</td>
              <td>{pools[i]}</td>
              <td>{pools[i] === 0 ? "—" : `${multipliers[i].toFixed(2)}×`}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <p>Total pool: {total}</p>
    </div>
  );
}
