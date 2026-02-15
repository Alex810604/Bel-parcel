const base: string = import.meta.env.VITE_API_BASE_URL || 'http://localhost:8090'
const trackingBase: string = import.meta.env.VITE_TRACKING_BASE_URL || 'http://localhost:8087'

function authHeaders(): Record<string, string> {
  const t = localStorage.getItem('access_token') || ''
  return t ? { Authorization: `Bearer ${t}` } : {}
}

export async function getTrips(params: Record<string, string>) {
  try {
    const qp = new URLSearchParams(params)
    const r = await fetch(`${base}/trips?${qp.toString()}`, { headers: { ...authHeaders() } })
    if (r.ok) return r.json()
  } catch {}
  return [
    {
      id: 'trip-789',
      origin_warehouse_id: 'wh-1',
      pickup_point_id: 'pvp-minsk-1',
      carrier_id: 'carrier-demo',
      assigned_at: new Date().toISOString(),
      status: 'ASSIGNED',
    },
    {
      id: 'trip-456',
      origin_warehouse_id: 'wh-2',
      pickup_point_id: 'pvp-minsk-2',
      carrier_id: 'carrier-demo-2',
      assigned_at: new Date(Date.now() - 3600_000).toISOString(),
      status: 'PENDING',
    },
  ]
}

export async function getTripDetails(id: string) {
  try {
    const r = await fetch(`${base}/trips/${id}`, { headers: { ...authHeaders() } })
    if (r.ok) return r.json()
  } catch {}
  return {
    trip: {
      id,
      origin_warehouse_id: 'wh-1',
      pickup_point_id: 'pvp-minsk-1',
      carrier_id: 'carrier-demo',
      assigned_at: new Date().toISOString(),
      status: 'ASSIGNED',
    },
    batches: ['batch-demo-1'],
    orders: ['order-001', 'order-002'],
  }
}

export async function getDelays(hours: number) {
  try {
    const r = await fetch(`${base}/delays?hours=${hours}`, { headers: { ...authHeaders() } })
    if (r.ok) return r.json()
  } catch {}
  return [
    {
      id: 'trip-456',
      origin_warehouse_id: 'wh-2',
      pickup_point_id: 'pvp-minsk-2',
      carrier_id: 'carrier-demo-2',
      assigned_at: new Date(Date.now() - 2 * 3600_000).toISOString(),
      status: 'PENDING',
    },
  ]
}

export async function searchRefs(type: string, q: string) {
  const r = await fetch(`${base}/references/${type}?q=${encodeURIComponent(q)}`, { headers: { ...authHeaders() } })
  return r.json()
}

export async function reassignTrip(id: string, newCarrierId: string, reason: string, operatorId: string) {
  const r = await fetch(`${base}/trips/${id}/reassign`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ new_carrier_id: newCarrierId, reason, operator_id: operatorId })
  })
  return r.ok
}

export async function getCarrierLocations() {
  const r = await fetch(`${trackingBase}/locations`)
  return r.json()
}
