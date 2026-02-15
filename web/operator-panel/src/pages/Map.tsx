import { useEffect, useState, useRef } from 'react'
import { Snackbar, Alert, Button } from '@mui/material'
import { useNavigate } from 'react-router-dom'
import { MapContainer, TileLayer, Marker, Popup } from 'react-leaflet'
import 'leaflet/dist/leaflet.css'
import { getCarrierLocations } from '../api'

type Location = {
  trip_id: string
  lat: number
  lng: number
  updated_at: string
}

export default function MapPage() {
  const [locations, setLocations] = useState<Location[]>([])
  const wsRef = useRef<WebSocket | null>(null)
  const [alertOpen, setAlertOpen] = useState(false)
  const [alertTripId, setAlertTripId] = useState<string>('')
  const [alertMessage, setAlertMessage] = useState<string>('')
  const navigate = useNavigate()
  const [isOnline, setIsOnline] = useState<boolean>(true)

  async function load() {
    const data = await getCarrierLocations()
    setLocations(Array.isArray(data) ? data : [])
  }

  function playAlertSound() {
    const enabled = (localStorage.getItem('sound_enabled') ?? 'true') !== 'false'
    if (!enabled) return
    try {
      const audio = new Audio('/alert.mp3')
      audio.play().catch(() => {
        try {
          const Ctx = (window as any).AudioContext || (window as any).webkitAudioContext
          const ctx = new Ctx()
          const osc = ctx.createOscillator()
          const gain = ctx.createGain()
          osc.type = 'sine'
          osc.frequency.value = 880
          gain.gain.value = 0.05
          osc.connect(gain)
          gain.connect(ctx.destination)
          osc.start()
          setTimeout(() => { osc.stop(); ctx.close() }, 200)
        } catch {}
      })
    } catch {}
  }

  function incrementAlertCounter() {
    const curr = parseInt(localStorage.getItem('alert_count') || '0', 10) || 0
    const next = curr + 1
    localStorage.setItem('alert_count', String(next))
    try {
      window.dispatchEvent(new CustomEvent('alert:new', { detail: { count: next } }))
    } catch {}
  }

  useEffect(() => {
    load()
    const base = import.meta.env.VITE_TRACKING_WS_URL || (import.meta.env.VITE_TRACKING_BASE_URL || 'http://localhost:8087').replace('http', 'ws') + '/ws/locations'
    function connect() {
      const ws = new WebSocket(base)
      wsRef.current = ws
      ws.onopen = () => setIsOnline(true)
      ws.onclose = () => {
        setIsOnline(false)
        setTimeout(connect, 2000)
      }
      ws.onmessage = (ev) => {
        try {
          const msg = JSON.parse(ev.data)
          if (msg.type === 'alert' && msg.payload?.trip_id) {
            setAlertTripId(msg.payload.trip_id)
            setAlertMessage(msg.payload?.message || 'Рейс требует ручного назначения')
            setAlertOpen(true)
            playAlertSound()
            incrementAlertCounter()
            return
          }
          if (typeof msg.trip_id === 'string') {
            setLocations(prev => {
              const idx = prev.findIndex(p => p.trip_id === msg.trip_id)
              const item = { trip_id: msg.trip_id, lat: msg.lat, lng: msg.lng, updated_at: msg.timestamp }
              if (idx >= 0) {
                const next = prev.slice()
                next[idx] = item
                return next
              }
              return [...prev, item]
            })
          }
        } catch {}
      }
      ws.onerror = () => {}
    }
    connect()
    return () => { wsRef.current?.close() }
  }, [])

  return (
    <>
      <div style={{ height: '80vh', width: '100%', position: 'relative' }}>
        {!isOnline && (
          <div style={{ position: 'absolute', top: 10, right: 10, background: '#d32f2f', color: '#fff', padding: '6px 10px', borderRadius: 6, zIndex: 1000 }}>
            Офлайн
          </div>
        )}
        <MapContainer center={[53.9, 27.5667]} zoom={11} style={{ height: '100%', width: '100%' }}>
          <TileLayer url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png" />
          {locations.map(loc => (
            <Marker key={loc.trip_id} position={[loc.lat, loc.lng]}>
              <Popup>
                Рейс: {loc.trip_id}
                <br />
                Обновлено: {new Date(loc.updated_at).toLocaleString('ru-RU')}
              </Popup>
            </Marker>
          ))}
        </MapContainer>
      </div>
      <Snackbar open={alertOpen} autoHideDuration={6000} onClose={() => setAlertOpen(false)}>
        <Alert
          severity="error"
          onClose={() => setAlertOpen(false)}
          action={
            <Button color="inherit" size="small" onClick={() => navigate(`/trips/${alertTripId}`)}>
              Открыть рейс
            </Button>
          }
        >
          Требуется вмешательство — Рейс {alertTripId} не назначен. Причина: {alertMessage}
        </Alert>
      </Snackbar>
    </>
  )
}
