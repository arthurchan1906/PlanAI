try:
    from .run_server import main
except ImportError:
    from run_server import main


if __name__ == "__main__":
    main()
