import { useEffect, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { Box, Typography, TextField, Button } from '@mui/material'
import { getTripDetails, searchRefs, reassignTrip } from '../api'

export default function TripDetails() {
  const { tripId } = useParams()
  const [details, setDetails] = useState<any>(null)
  const [carrierQ, setCarrierQ] = useState('')
  const [carriers, setCarriers] = useState<any[]>([])
  const [newCarrier, setNewCarrier] = useState('')
  const [reason, setReason] = useState('')
  const navigate = useNavigate()

  async function load() {
    if (!tripId) return
    const d = await getTripDetails(tripId)
    setDetails(d)
  }

  useEffect(() => { load() }, [tripId])

  async function findCarriers() {
    const res = await searchRefs('carriers', carrierQ)
    setCarriers(res)
  }

  async function submit() {
    if (!tripId || !newCarrier || !reason) return
    const ok = await reassignTrip(tripId, newCarrier, reason, 'operator_1')
    if (ok) navigate('/')
  }

  if (!details) return null

  return (
    <Box>
      <Typography variant="h6" sx={{ mb: 2 }}>Рейс {tripId}</Typography>
      <Typography>Статус: {details.trip.status}</Typography>
      <Typography>Склад: {details.trip.origin_warehouse_id}</Typography>
      <Typography>ПВЗ: {details.trip.pickup_point_id}</Typography>
      <Typography sx={{ mt: 2 }}>Партии: {(details.batches || []).join(', ')}</Typography>
      <Typography>Заказы: {(details.orders || []).join(', ')}</Typography>
      <Box sx={{ mt: 3, display: 'flex', gap: 2 }}>
        <TextField label="Поиск перевозчика" value={carrierQ} onChange={e => setCarrierQ(e.target.value)} />
        <Button variant="outlined" onClick={findCarriers}>Найти</Button>
      </Box>
      <Box sx={{ mt: 2 }}>
        <TextField label="Новый перевозчик ID" value={newCarrier} onChange={e => setNewCarrier(e.target.value)} sx={{ mr: 2 }} />
        {carriers.length > 0 && <Box sx={{ mb: 1 }}>
          <Typography>Найдено:</Typography>
          {carriers.map(c => (
            <Button key={c.id} size="small" sx={{ mr: 1, mb: 1 }} onClick={() => setNewCarrier(c.id)}>{c.name}</Button>
          ))}
        </Box>}
        <TextField label="Причина" value={reason} onChange={e => setReason(e.target.value)} sx={{ mr: 2 }} />
        <Button variant="contained" onClick={submit}>Переназначить</Button>
      </Box>
    </Box>
  )
}
