package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
	_ "github.com/lib/pq"
	"github.com/myesui/uuid"
)

func getPort() string {
	// the PORT is supplied by Heroku
	port := os.Getenv("PORT")
	if port == "" {
		return ":5551"
	}
	return ":" + port
}

func failOnError(err error, msg string) {
	if err != nil {
		fmt.Printf("%s: %+v\n ", msg, err)
		os.Exit(1)
	}
}

var errNotFound = errors.New("Error: No data found")

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	db, err := sql.Open("postgres", dbURL)
	failOnError(err, "failed to connect db")
	defer db.Close()

	err = db.Ping()
	failOnError(err, "failed to connect db")

	e := echo.New()
	e.Use(middleware.RequestID())
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	serives, err := NewRepositories(db)
	failOnError(err, "failed to connect db")

	country := NewCountryService(serives.CountryRepo, serives.ProvinceRepo)
	province := NewProvinceService(serives.ProvinceRepo)

	e.GET("/api/v1/country/:country_id", country.FindByCountryID)
	e.POST("/api/v1/country", country.Store)
	e.PUT("/api/v1/country/:country_id", country.Edit)
	e.PUT("/api/v1/province/:province_id", province.UpdateProvince)

	if err := e.Start(getPort()); err != nil && err != http.ErrServerClosed {
		fmt.Print(err)
		os.Exit(1)
	}
	defer serives.Close()
}

// Application
type countryApp struct {
	cApp CountryRepository
}

type CountryAppInterface interface {
	Save(ctx context.Context, c *Country) error
	Update(ctx context.Context, c *Country) error
	Delete(ctx context.Context, c *Country) error
	GetByID(ctx context.Context, id string) (*Country, error)
}

var _ CountryAppInterface = &countryRepo{}

func (ca *countryApp) Save(ctx context.Context, ct *Country) error {
	return ca.cApp.Save(ctx, ct)
}
func (ca *countryApp) Update(ctx context.Context, ct *Country) error {
	return ca.cApp.Update(ctx, ct)
}
func (ca *countryApp) Delete(ctx context.Context, ct *Country) error {
	return ca.cApp.Delete(ctx, ct)
}
func (ca *countryApp) GetByID(ctx context.Context, id string) (*Country, error) {
	return ca.cApp.GetByID(ctx, id)
}

type provinceApp struct {
	pApp ProvinceRepository
}

type ProvinceInterface interface {
	Save(ctx context.Context, p *Province) error
	Update(ctx context.Context, p *Province) error
	Delete(ctx context.Context, p *Province) error
	GetByID(ctx context.Context, id string) (*Province, error)
	GetAll(ctx context.Context) (Provinces, error)
}

var _ ProvinceInterface = &provinceApp{}

func (pa *provinceApp) Save(ctx context.Context, p *Province) error {
	return pa.pApp.Save(ctx, p)
}
func (pa *provinceApp) Update(ctx context.Context, p *Province) error {
	return pa.pApp.Update(ctx, p)
}
func (pa *provinceApp) Delete(ctx context.Context, p *Province) error {
	return pa.pApp.Delete(ctx, p)
}
func (pa *provinceApp) GetByID(ctx context.Context, id string) (*Province, error) {
	return pa.pApp.GetByID(ctx, id)
}
func (pa *provinceApp) GetAll(ctx context.Context) (Provinces, error) {
	return pa.pApp.GetAll(ctx)
}

// new handler
type countryService struct {
	cApp CountryAppInterface
	pApp ProvinceInterface
}

type provinceService struct {
	pApp ProvinceInterface
}

type ErrorMsg struct {
	Msg string `json:"error"`
}

type SuccessResponse struct {
	Msg string `json:"success"`
}

func NewCountryService(cApp CountryAppInterface, pApp ProvinceInterface) *countryService {
	return &countryService{cApp: cApp, pApp: pApp}
}

func (cA *countryService) errMessage(err string) *ErrorMsg {
	return &ErrorMsg{err}
}

func (cA *countryService) successMsg(success string) *SuccessResponse {
	return &SuccessResponse{success}
}

func (cA *countryService) FindByCountryID(c echo.Context) error {
	country, err := cA.cApp.GetByID(c.Request().Context(),
		html.EscapeString(strings.TrimSpace(c.Param("country_id"))))
	if err == errNotFound {
		return c.JSON(http.StatusNotFound, cA.errMessage(err.Error()))
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, cA.errMessage("Internal server error"))
	}
	return c.JSON(http.StatusOK, map[string]*Country{"country": country})
}

func (cA *countryService) Store(c echo.Context) error {
	var country Country
	if err := c.Bind(&country); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, cA.errMessage("request: unable to parse request payload"))
	}
	country.Prepare()
	country.BeforeSave()
	country.UpdatedAt = time.Now()
	if err := country.Validate(); err != nil {
		return c.JSON(http.StatusBadRequest, cA.errMessage(err.Error()))
	}

	for _, p := range country.Provinces {
		p.Prepare()
		p.BeforeSave()
		p.UpdatedAt = time.Now()
		if err := p.Validate(); err != nil {
			return c.JSON(http.StatusBadRequest, cA.errMessage(err.Error()))
		}
	}

	if err := cA.cApp.Save(c.Request().Context(), &country); err != nil {
		return c.JSON(http.StatusInternalServerError, cA.errMessage("Internal server error"))
	}

	return c.JSON(http.StatusOK, map[string]*Country{"country": &country})
}

func (cA *countryService) Edit(c echo.Context) error {
	var country Country
	if err := c.Bind(&country); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, cA.errMessage("request: unable to parse request payload"))
	}
	country.Prepare()
	country.UpdatedAt = time.Now()
	if err := country.Validate(); err != nil {
		return c.JSON(http.StatusBadRequest, cA.errMessage(err.Error()))
	}

	for _, p := range country.Provinces {
		if err := p.Validate(); err != nil {
			return c.JSON(http.StatusBadRequest, cA.errMessage(err.Error()))
		}
	}

	for _, p := range country.Provinces {
		p.Prepare()
		p.UpdatedAt = time.Now()
		if err := cA.pApp.Update(c.Request().Context(), p); err != nil {
			return c.JSON(http.StatusInternalServerError, cA.errMessage("Internal server error, could not update province information"))
		}
	}

	if err := cA.cApp.Update(c.Request().Context(), &country); err != nil {
		return c.JSON(http.StatusInternalServerError, cA.errMessage("Internal server error"))
	}

	return c.JSON(http.StatusOK, map[string]*Country{"country": &country})
}

func NewProvinceService(pApp ProvinceInterface) *provinceService {
	return &provinceService{pApp: pApp}
}

func (pA *provinceService) errMessage(err string) *ErrorMsg {
	return &ErrorMsg{err}
}

func (pA *provinceService) successMsg(success string) *SuccessResponse {
	return &SuccessResponse{success}
}

func (pA *provinceService) UpdateProvince(c echo.Context) error {
	var p Province
	if err := c.Bind(&p); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, pA.errMessage("request: unable to parse request payload"))
	}
	p.Prepare()
	p.UpdatedAt = time.Now()
	if err := p.Validate(); err != nil {
		return c.JSON(http.StatusBadRequest, pA.errMessage(err.Error()))
	}

	if err := pA.pApp.Update(c.Request().Context(), &p); err != nil {
		return c.JSON(http.StatusInternalServerError, pA.errMessage("Internal server error, could not update province information"))
	}
	return c.JSON(http.StatusOK, map[string]*Province{"province": &p})
}

// data model
type District struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Total          int64     `json:"total"`
	NewCase        int64     `json:"new_case"`
	Treated        int64     `json:"treaded"`
	DecoveringCase int64     `json:"decovering_case"`
	TestCase       int64     `json:"test_case"`
	Dead           int64     `json:"dead"`
	NegativeTest   int64     `json:"negative_case"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Districts []*District

func (d *District) Prepare() {
	d.ID = uuid.NewV4().String()
	d.Name = html.EscapeString(strings.TrimSpace(d.Name))
}

func (d *District) BeforeSave() {
	d.ID = uuid.NewV4().String()
}

func (d *District) Validate() error {
	if d.Name == "" {
		return errors.New("district: name is required")
	}
	return nil
}

type Province struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Total          int64     `json:"total"`
	NewCase        int64     `json:"new_case"`
	Treated        int64     `json:"treaded"`
	DecoveringCase int64     `json:"decovering_case"`
	TestCase       int64     `json:"test_case"`
	Dead           int64     `json:"dead"`
	NegativeTest   int64     `json:"negative_case"`
	Districts      Districts `json:"districts"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Provinces []*Province

func (p *Province) Prepare() {
	p.Name = html.EscapeString(strings.TrimSpace(p.Name))
}

func (p *Province) BeforeSave() {
	p.ID = uuid.NewV4().String()
}

func (p *Province) Validate() error {
	if p.Name == "" {
		return errors.New("province: name is required")
	}
	return nil
}

type Country struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Total          int64     `json:"total"`
	NewCase        int64     `json:"new_case"`
	Treated        int64     `json:"treaded"`
	DecoveringCase int64     `json:"decovering_case"`
	TestCase       int64     `json:"test_case"`
	NegativeTest   int64     `json:"negative_case"`
	Dead           int64     `json:"dead"`
	Provinces      Provinces `json:"provinces"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Countries []*Country

func (c *Country) Prepare() {
	c.Name = html.EscapeString(strings.TrimSpace(c.Name))
}

func (c *Country) BeforeSave() {
	c.ID = uuid.NewV4().String()
}

func (c *Country) Validate() error {
	if c.Name == "" {
		return errors.New("country: name is required")
	}
	return nil
}

// Repository
type CountryRepository interface {
	Save(ctx context.Context, c *Country) error
	Update(ctx context.Context, c *Country) error
	Delete(ctx context.Context, c *Country) error
	GetByID(ctx context.Context, id string) (*Country, error)
}

type ProvinceRepository interface {
	Save(ctx context.Context, p *Province) error
	Update(ctx context.Context, p *Province) error
	Delete(ctx context.Context, p *Province) error
	GetByID(ctx context.Context, id string) (*Province, error)
	GetAll(ctx context.Context) (Provinces, error)
}

type DistrictRepository interface {
	Save(ctx context.Context, c *Country) error
	Update(ctx context.Context, c *Country) error
	Delete(ctx context.Context, c *Country) error
	GetByID(ctx context.Context, id string) (*Country, error)
	GetAll(ctx context.Context) (Countries, error)
}

type Repository struct {
	CountryRepo  CountryRepository
	ProvinceRepo ProvinceRepository
	DistrictRepo DistrictRepository
	DB           *sql.DB
}

func NewRepositories(db *sql.DB) (*Repository, error) {
	return &Repository{
		CountryRepo:  NewCountryRepo(db),
		ProvinceRepo: NewProvinceRepo(db),
		DistrictRepo: NewDistrictRepo(db),
	}, nil
}

func (r *Repository) Close() error {
	return r.DB.Close()
}

// Country Repo
type countryRepo struct {
	db *sql.DB
}

var _ CountryRepository = &countryRepo{}

func NewCountryRepo(db *sql.DB) *countryRepo {
	return &countryRepo{db}
}

func (cr *countryRepo) Save(ctx context.Context, c *Country) error {
	tx, err := cr.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer func() {
		if err == nil {
			if commitErr := tx.Commit(); commitErr != nil {
				return
			}
		}
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return
		}
	}()

	if _, err := squirrel.Insert("country").
		Columns("id",
			"name",
			"total",
			"new_case",
			"treated",
			"decovering_case",
			"test_case",
			"dead",
			"negative_case",
			"updated_at").
		Values(&c.ID,
			&c.Name,
			&c.Total,
			&c.NewCase,
			&c.Treated,
			&c.DecoveringCase,
			&c.TestCase,
			&c.Dead,
			&c.NegativeTest,
			&c.UpdatedAt).
		PlaceholderFormat(squirrel.Dollar).
		RunWith(tx).ExecContext(ctx); err != nil {
		return err
	}

	stmProvince := squirrel.Insert("provinces").
		Columns("id",
			"name",
			"total",
			"new_case",
			"treated",
			"decovering_case",
			"test_case",
			"dead",
			"negative_case",
			"country_id",
			"updated_at")
	for _, p := range c.Provinces {
		stmProvince = stmProvince.Values(&p.ID,
			&p.Name,
			&p.Total,
			&p.NewCase,
			&p.Treated,
			&p.DecoveringCase,
			&p.TestCase,
			&p.Dead,
			&p.NegativeTest,
			&c.ID,
			&p.UpdatedAt)
	}

	if _, err := stmProvince.PlaceholderFormat(squirrel.Dollar).RunWith(tx).ExecContext(ctx); err != nil {
		return err
	}

	return nil
}
func (cr *countryRepo) Update(ctx context.Context, c *Country) error {
	if _, err := squirrel.Update("country").
		Set("name", &c.Name).
		Set("total", &c.Total).
		Set("new_case", &c.NewCase).
		Set("treated", &c.Treated).
		Set("decovering_case", &c.DecoveringCase).
		Set("test_case", &c.TestCase).
		Set("dead", &c.Dead).
		Set("negative_case", &c.NegativeTest).
		Set("updated_at", &c.UpdatedAt).
		Where(squirrel.Eq{"id": &c.ID}).
		PlaceholderFormat(squirrel.Dollar).
		RunWith(cr.db).ExecContext(ctx); err != nil {
		return err
	}

	return nil
}
func (cr *countryRepo) Delete(ctx context.Context, c *Country) error {
	tx, err := cr.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer func() {
		if err == nil {
			if commitErr := tx.Commit(); commitErr != nil {
				return
			}
		}
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return
		}
	}()

	if _, err := squirrel.Delete("country").
		Where(squirrel.Eq{"id": &c.ID}).
		PlaceholderFormat(squirrel.Dollar).
		RunWith(tx).ExecContext(ctx); err != nil {
		return err
	}

	if _, err := squirrel.Delete("provinces").
		Where(squirrel.Eq{"country_id": &c.ID}).
		PlaceholderFormat(squirrel.Dollar).
		RunWith(tx).ExecContext(ctx); err != nil {
		return err
	}

	return nil
}
func (cr *countryRepo) GetByID(ctx context.Context, id string) (*Country, error) {
	var c Country
	err := squirrel.Select("id",
		"name",
		"total",
		"new_case",
		"treated",
		"decovering_case",
		"test_case",
		"dead",
		"negative_case",
		"updated_at").From("country").
		Where(squirrel.Eq{"id": id}).
		PlaceholderFormat(squirrel.Dollar).
		RunWith(cr.db).ScanContext(ctx,
		&c.ID,
		&c.Name,
		&c.Total,
		&c.NewCase,
		&c.Treated,
		&c.DecoveringCase,
		&c.TestCase,
		&c.Dead,
		&c.NegativeTest,
		&c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}

	rows, err := squirrel.Select("id",
		"name",
		"total",
		"new_case",
		"treated",
		"decovering_case",
		"test_case",
		"dead",
		"negative_case",
		"updated_at").
		From("provinces").
		Where(squirrel.Eq{"country_id": id}).
		OrderBy("total DESC").
		PlaceholderFormat(squirrel.Dollar).
		RunWith(cr.db).QueryContext(ctx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ps = make(Provinces, 0)
	for rows.Next() {
		var p Province
		if err := rows.Scan(&p.ID,
			&p.Name,
			&p.Total,
			&p.NewCase,
			&p.Treated,
			&p.DecoveringCase,
			&p.TestCase,
			&p.Dead,
			&p.NegativeTest,
			&p.UpdatedAt); err != nil {
			return nil, err
		}
		ps = append(ps, &p)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	c.Provinces = ps

	return &c, nil
}

// Province Repo
type provinceRepo struct {
	db *sql.DB
}

var _ ProvinceRepository = &provinceRepo{}

func NewProvinceRepo(db *sql.DB) *provinceRepo {
	return &provinceRepo{db}
}

func (pr *provinceRepo) Save(ctx context.Context, p *Province) error {
	return nil
}
func (pr *provinceRepo) Update(ctx context.Context, p *Province) error {
	_, err := squirrel.Update("provinces").
		Set("name", &p.Name).
		Set("total", &p.Total).
		Set("treated", &p.Treated).
		Set("decovering_case", &p.DecoveringCase).
		Set("test_case", &p.TestCase).
		Set("dead", &p.Dead).
		Set("negative_case", &p.NegativeTest).
		Set("updated_at", &p.UpdatedAt).
		Where(squirrel.Eq{"id": &p.ID}).
		PlaceholderFormat(squirrel.Dollar).
		RunWith(pr.db).ExecContext(ctx)
	if err != nil {
		return err
	}
	return nil
}
func (pr *provinceRepo) Delete(ctx context.Context, p *Province) error {
	return nil
}
func (pr *provinceRepo) GetByID(ctx context.Context, id string) (*Province, error) {
	return nil, nil
}
func (pr *provinceRepo) GetAll(ctx context.Context) (Provinces, error) {
	return nil, nil
}

// District Repo
type districtRepo struct {
	db *sql.DB
}

var _ DistrictRepository = &districtRepo{}

func NewDistrictRepo(db *sql.DB) *districtRepo {
	return &districtRepo{db}
}

func (dr *districtRepo) Save(ctx context.Context, c *Country) error {
	return nil
}
func (dr *districtRepo) Update(ctx context.Context, c *Country) error {
	return nil
}
func (dr *districtRepo) Delete(ctx context.Context, c *Country) error {
	return nil
}
func (dr *districtRepo) GetByID(ctx context.Context, id string) (*Country, error) {
	return nil, nil
}
func (dr *districtRepo) GetAll(ctx context.Context) (Countries, error) {
	return nil, nil
}
