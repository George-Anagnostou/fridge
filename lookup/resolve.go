package lookup

import (
	"context"
)

// Resolver combines cache, Open Food Facts, and an optional USDA fallback.
// USDA is used to fill gaps in an OFF hit, or on its own when OFF misses.
// When both sources have a value for the same field, the OFF value wins.
type Resolver struct {
	Cache *Cache
	OFF   Source
	USDA  Source
}

func (r *Resolver) Lookup(ctx context.Context, barcode string) (Result, error) {
	norm, err := NormalizeBarcode(barcode)
	if err != nil {
		return Result{}, err
	}

	offRes, offErr := r.fetch(ctx, norm, SourceOFF, r.OFF)
	if offErr == nil && offRes.Found && productComplete(offRes.Product) {
		return offRes, nil
	}

	usdaRes, usdaErr := r.fetch(ctx, norm, SourceUSDA, r.USDA)
	if offErr == nil && offRes.Found {
		if usdaErr == nil && usdaRes.Found {
			offRes.Product = mergeProduct(offRes.Product, usdaRes.Product)
		}
		return offRes, nil
	}
	if usdaErr == nil && usdaRes.Found {
		return usdaRes, nil
	}
	if offErr != nil {
		return Result{}, offErr
	}
	if usdaErr != nil {
		return Result{}, usdaErr
	}
	return Result{Found: false, Product: Product{Barcode: norm}}, nil
}

func (r *Resolver) fetch(ctx context.Context, barcode, source string, src Source) (Result, error) {
	if r.Cache == nil {
		if src == nil {
			return Result{Found: false, Product: Product{Barcode: barcode}}, nil
		}
		return src.Lookup(ctx, barcode)
	}
	if src == nil {
		res, ok, err := r.Cache.Get(ctx, barcode, source)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			return Result{Found: false, Product: Product{Barcode: barcode}}, nil
		}
		return res, nil
	}
	return r.Cache.GetOrFetch(ctx, barcode, source, src)
}

func productComplete(p Product) bool {
	return p.Name != "" && p.Brand != "" && p.ServingSize != "" &&
		p.Calories != nil && p.ProteinG != nil && p.CarbsG != nil && p.FatG != nil
}

// mergeProduct fills empty fields in primary from fallback. Primary values win.
func mergeProduct(primary, fallback Product) Product {
	out := primary
	if out.Name == "" {
		out.Name = fallback.Name
	}
	if out.Brand == "" {
		out.Brand = fallback.Brand
	}
	if out.ServingSize == "" {
		out.ServingSize = fallback.ServingSize
	}
	if out.Calories == nil {
		out.Calories = fallback.Calories
	}
	if out.ProteinG == nil {
		out.ProteinG = fallback.ProteinG
	}
	if out.CarbsG == nil {
		out.CarbsG = fallback.CarbsG
	}
	if out.FatG == nil {
		out.FatG = fallback.FatG
	}
	if out.Source == "" {
		out.Source = fallback.Source
	}
	if out.Barcode == "" {
		out.Barcode = fallback.Barcode
	}
	return out
}
