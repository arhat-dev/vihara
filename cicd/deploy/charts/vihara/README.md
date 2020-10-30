# vihara

`vihara` is a maintenance controller for kubernetes

## Introduction

This is the official helm chart for [vihara](https://github.com/arhat-dev/vihara), you can deploy `vihara` and create `Maintenance` to execute maintenance jobs on kubernetes nodes

## Prerequisites

- `helm` v3
- `Kubernetes` 1.15+

## Installing the Chart

```bash
helm install my-release arhat-dev/vihara
```

## Uninstalling the Chart

```bash
helm delete my-release
```

## Configuration

Please refer to the [`values.yaml`](https://github.com/arhat-dev/vihara/blob/master/cicd/deploy/charts/vihara/values.yaml)
