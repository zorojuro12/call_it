type Player = { user_id: string; display_name: string };

export function PresenceRoster({
  players,
  selfId,
}: {
  players: Player[];
  selfId: string | null;
}) {
  return (
    <ul>
      {players.map((p) => (
        <li key={p.user_id}>
          {p.display_name}
          {p.user_id === selfId ? " (you)" : ""}
        </li>
      ))}
    </ul>
  );
}
