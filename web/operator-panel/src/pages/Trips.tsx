import { useEffect, useState } from 'react'
import { DataGrid, GridColDef } from '@mui/x-data-grid'
import { Box, TextField, MenuItem, Button, Typography } from '@mui/material'
import { getTrips, getDelays } from '../api'
import { useNavigate } from 'react-router-dom'

type TripRow = {
  id: string
  origin_warehouse_id: string
  pickup_point_id: string
  carrier_id: string
  assigned_at: string
  status: string
}

export default function Trips() {
  const [rows, setRows] = useState<TripRow[]>([])
  const [status, setStatus] = useState('')
  const [pvz, setPvz] = useState('')
  const [carrier, setCarrier] = useState('')
  const [date, setDate] = useState('')
  const [delaysCount, setDelaysCount] = useState(0)
  const navigate = useNavigate()

  const cols: GridColDef[] = [
    { field: 'id', headerName: 'ID рейса', width: 180 },
    { field: 'origin_warehouse_id', headerName: 'Склад', width: 160 },
    { field: 'pickup_point_id', headerName: 'ПВЗ', width: 160 },
    { field: 'carrier_id', headerName: 'Перевозчик', width: 160 },
    { field: 'assigned_at', headerName: 'Назначен', width: 180 },
    { field: 'status', headerName: 'Статус', width: 140 },
    {
      field: 'actions',
      headerName: 'Действия',
      sortable: false,
      width: 180,
      renderCell: p => (
        <Button variant="outlined" size="small" onClick={() => navigate(`/trips/${p.row.id}`)}>
          Переназначить
        </Button>
      )
    }
  ]

  async function load() {
    const data = await getTrips({ status, pvz, carrier, date })
    setRows(data)
    const d = await getDelays(1.5)
    setDelaysCount(d.length || 0)
  }

  useEffect(() => { load() }, [])

  return (
    <Box>
      <Box sx={{ display: 'flex', gap: 2, mb: 2 }}>
        <TextField select label="Статус" value={status} onChange={e => setStatus(e.target.value)} sx={{ minWidth: 160 }}>
          <MenuItem value="">Все</MenuItem>
          <MenuItem value="assigned">assigned</MenuItem>
          <MenuItem value="in_transit">in_transit</MenuItem>
          <MenuItem value="failed">failed</MenuItem>
        </TextField>
        <TextField label="ПВЗ" value={pvz} onChange={e => setPvz(e.target.value)} />
        <TextField label="Перевозчик" value={carrier} onChange={e => setCarrier(e.target.value)} />
        <TextField type="date" label="Дата" InputLabelProps={{ shrink: true }} value={date} onChange={e => setDate(e.target.value)} />
        <Button variant="contained" onClick={load}>Применить</Button>
      </Box>
      <Typography color="error" sx={{ mb: 1 }}>
        Внимание: {delaysCount} рейса в статусе "assigned" более 1.5 часов.
      </Typography>
      <div style={{ height: 520, width: '100%' }}>
        <DataGrid rows={rows} columns={cols} getRowId={(r) => r.id} />
      </div>
    </Box>
  )
}
